"""Exercise the workflow's display replacement against real Git trees."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

import yaml

from susfs_guard_priority_helpers import mark_optional_helpers


ROOT = Path(__file__).resolve().parents[2]


class DisplayAlignmentTests(unittest.TestCase):
    def test_bakasu_susfs_credential_helpers_remain_visible_to_path_filter(self):
        source = (
            'static struct cred *susfs_switch_to_ksu_domain(void) {\n    return NULL;\n}\n'
            'static void susfs_restore_creds(struct cred *orig_cred) {\n}\n'
            '#ifdef CONFIG_KSU_SUSFS_SUS_PATH\n'
            '    struct cred *orig = susfs_switch_to_ksu_domain();\n'
            '    susfs_restore_creds(orig);\n'
            '#endif\n'
        )
        expected = source.replace('static ', 'static __maybe_unused ')
        self.assertEqual(mark_optional_helpers(source), expected)
        self.assertEqual(mark_optional_helpers(expected), expected)

    def test_donor_replaces_tree_including_added_and_removed_files(self):
        workflow = yaml.safe_load((ROOT / '.github/workflows/build.yml').read_text(encoding='utf-8'))
        step = next(s for s in workflow['jobs']['build-gki']['steps']
                    if s.get('name', '').startswith('Surgical 6.1-LTS'))
        script = step['run'].split('DISPLAY_PATHS=(', 1)[1].split('# Prevent -Wvla', 1)[0]
        script = 'set -euo pipefail\nDISPLAY_PATHS=(' + script
        bash = shutil.which('bash')
        if os.name == 'nt':
            bash = str(Path(os.environ['ProgramFiles']) / 'Git/bin/bash.exe')
        with tempfile.TemporaryDirectory(prefix='houji-display-') as directory:
            repo = Path(directory)

            def git(*args):
                return subprocess.check_output(['git', '-C', directory, *args], text=True,
                                               stderr=subprocess.PIPE).strip()

            def write(path, data):
                target = repo / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(data, encoding='utf-8')

            git('init')
            git('config', 'user.name', 'Fixture')
            git('config', 'user.email', 'fixture@example.invalid')
            display = ['drivers/gpu/drm/shared.h', 'include/drm/shared.h',
                       'drivers/video/backlight/shared.c', 'include/linux/backlight.h',
                       'drivers/spmi/shared.c']
            protected = ['drivers/dma-buf/shared.c', 'fs/f2fs/shared.c',
                         'include/linux/dma-buf.h', 'include/linux/sync_file.h',
                         'include/linux/iosys-map.h']
            for path in display + protected:
                write(path, 'ACK177\n')
            ack_only = 'drivers/gpu/drm/i915/display/intel_dpcd.h'
            write(ack_only, 'ACK-only\n')
            git('add', '.')
            git('commit', '-m', 'ACK fixture')
            ack = git('rev-parse', 'HEAD')
            for path in display + protected:
                write(path, 'stock157\n')
            git('rm', ack_only)
            donor_only = 'drivers/gpu/drm/donor-only.h'
            write(donor_only, 'stock157\n')
            git('add', '.')
            git('commit', '-m', 'stock fixture')
            donor = git('rev-parse', 'HEAD')
            git('checkout', '--detach', ack)
            env = dict(os.environ, ACK_177_COMMIT=ack, STOCK_COMMIT=donor)
            result = subprocess.run([bash, '-c', script], cwd=repo, env=env,
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertFalse((repo / ack_only).exists())
            for path in display + [donor_only]:
                self.assertEqual((repo / path).read_text(), 'stock157\n')
            for path in protected:
                self.assertEqual((repo / path).read_text(), 'ACK177\n')

    def test_aligned_houji_skips_generic_zram_overrides(self):
        workflow = yaml.safe_load((ROOT / '.github/workflows/build.yml').read_text(encoding='utf-8'))
        steps = workflow['jobs']['build-gki']['steps']
        align = next(s for s in steps if s.get('id') == 'houji-alignment')
        misc = next(s for s in steps if s.get('uses') == './.github/actions/misc')
        self.assertIn('echo "applied=true" >> "$GITHUB_OUTPUT"', align['run'])
        self.assertIn('steps.houji-alignment.outputs.applied', misc['with']['preserve_zram_defaults'])

        action = yaml.safe_load((ROOT / '.github/actions/misc/action.yml').read_text(encoding='utf-8'))
        generic, zram = action['runs']['steps']
        self.assertNotIn('CONFIG_ZRAM', generic['with']['config_list'])
        self.assertEqual(zram['if'], "inputs.preserve_zram_defaults != 'true'")
        self.assertEqual(zram['with']['config_list'].splitlines(), [
            'CONFIG_ZRAM_DEF_COMP_LZ4=y', 'CONFIG_ZRAM_WRITEBACK=y', 'CONFIG_ZRAM_MEMORY_TRACKING=y',
        ])

    def test_stock_157_uses_exact_source_and_houji_config_checks(self):
        workflow = yaml.safe_load((ROOT / '.github/workflows/build.yml').read_text(encoding='utf-8'))
        steps = workflow['jobs']['build-gki']['steps']
        stock = next(s for s in steps if s.get('id') == 'houji-stock')
        misc = next(s for s in steps if s.get('uses') == './.github/actions/misc')
        verify = next(s for s in steps if s.get('name') == 'Verify houji kernel configuration in Image')
        self.assertEqual(stock['if'], "inputs.version == 'android14-6.1' && inputs.sublevel == '157' "
                         "&& inputs.os_patch_level == '2025-12'")
        self.assertIn('stock_commit=78a0a76cd7b7029059bdf03eba04ebeaf9af4eed', stock['run'])
        self.assertIn('git checkout --detach "$stock_commit"', stock['run'])
        self.assertIn('test "$(make -s kernelversion)" = \'6.1.157\'', stock['run'])
        self.assertIn('steps.houji-stock.outputs.applied', misc['with']['preserve_zram_defaults'])
        self.assertIn('steps.houji-stock.outputs.applied', verify['if'])

    def test_performance_keeps_ack_math_and_stock_pcm(self):
        workflow = yaml.safe_load((ROOT / '.github/workflows/build.yml').read_text(encoding='utf-8'))
        performance = next(s for s in workflow['jobs']['build-gki']['steps']
                           if s.get('uses') == './.github/actions/performance')
        self.assertEqual(performance['with']['houji_baseline'],
                         "${{ steps.houji-stock.outputs.applied == 'true' && 'stock' || "
                         "steps.houji-alignment.outputs.applied == 'true' && 'lts' || '' }}")

        path = ROOT / '.github/actions/performance/action.yml'
        action = yaml.safe_load(path.read_text(encoding='utf-8'))
        step = action['runs']['steps'][0]
        self.assertEqual(step['env']['HOUJI_BASELINE'], '${{ inputs.houji_baseline }}')
        script = (step['run'].replace('${{ inputs.kernel_version }}', '$TEST_KERNEL_VERSION')
                  .replace('${{ github.workspace }}', '$PWD')
                  .replace('${{ github.action_path }}', '$PWD'))
        bash = (str(Path(os.environ['ProgramFiles']) / 'Git/bin/bash.exe')
                if os.name == 'nt' else shutil.which('bash'))
        with tempfile.TemporaryDirectory(prefix='houji-performance-') as directory:
            repo = Path(directory)
            (repo / 'kernel_patches/common').mkdir(parents=True)
            (repo / 'patches').mkdir()
            for relative in set(re.findall(r'(?:kernel_patches/common|patches)/[\w-]+\.patch', script)):
                (repo / relative).write_text(Path(relative).stem + '\n', encoding='utf-8')
            for baseline, kernel, expected_math, expected_pcm in (
                ('stock', '6.1', False, False), ('lts', '6.1', False, True),
                ('', '6.1', True, True), ('', '5.15', True, True),
            ):
                with self.subTest(baseline=baseline, kernel=kernel):
                    env = dict(os.environ, HOUJI_BASELINE=baseline, TEST_KERNEL_VERSION=kernel)
                    result = subprocess.run([bash, '-c', 'patch() { cat; }\n' + script],
                                            cwd=repo, env=env, capture_output=True, text=True)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    applied = result.stdout.splitlines()
                    self.assertIn('mem_opt_prefetch', applied)
                    self.assertEqual('int_sqrt' in applied, expected_math)
                    self.assertEqual('0002-alsa-pcm-wait-time' in applied, expected_pcm)


if __name__ == '__main__':
    unittest.main()

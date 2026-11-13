"""Exercise the workflow's display replacement against real Git trees."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]


class DisplayAlignmentTests(unittest.TestCase):
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


if __name__ == '__main__':
    unittest.main()

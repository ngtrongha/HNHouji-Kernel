import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import URLError

import yaml

import resolve_susfs as resolver

SHA = "b" * 40
BRANCH = "gki-android14-6.1"
ROOT = Path(__file__).resolve().parents[2]


class ResolverTests(unittest.TestCase):
    def setUp(self):
        self.git = patch.object(resolver.subprocess, "run").start()
        self.api = patch.object(resolver, "urlopen").start()
        self.sleep = patch.object(resolver.time, "sleep").start()
        patch.object(resolver.sys, "stderr", io.StringIO()).start()
        self.addCleanup(patch.stopall)
        self.git.return_value.stdout = f"{SHA}\trefs/heads/{BRANCH}\n"
        self.api_response({"name": BRANCH, "commit": {"id": SHA}})

    def api_response(self, data):
        self.api.return_value.__enter__.return_value = io.StringIO(json.dumps(data))

    def test_git_success_does_not_call_api(self):
        self.assertEqual(resolver.resolve(BRANCH), SHA)
        self.api.assert_not_called()
        self.assertEqual(self.git.call_args.kwargs["timeout"], 45)

    def test_overload_or_timeout_uses_exact_official_branch_api(self):
        for error in [subprocess.CalledProcessError(128, "git"), subprocess.TimeoutExpired("git", 45)]:
            with self.subTest(error=error):
                self.git.side_effect = error
                self.api_response({"name": BRANCH, "commit": {"id": SHA}})
                self.assertEqual(resolver.resolve(BRANCH), SHA)
                self.api.assert_called_with(resolver.API + BRANCH, timeout=30)
        self.sleep.assert_not_called()

    def test_invalid_git_results_require_valid_api(self):
        for output in ["", f"{SHA}\trefs/heads/wrong", f"bad\trefs/heads/{BRANCH}"]:
            with self.subTest(output=output):
                self.git.return_value.stdout = output
                self.api_response({"name": BRANCH, "commit": {"id": SHA}})
                self.assertEqual(resolver.resolve(BRANCH), SHA)

    def test_invalid_api_results_never_produce_a_sha(self):
        self.git.side_effect = subprocess.CalledProcessError(128, "git")
        for data in [{}, [], {"name": "other", "commit": {"id": SHA}},
                     {"name": BRANCH, "commit": {"id": None}},
                     {"name": BRANCH, "commit": {"id": "bad"}}]:
            with self.subTest(data=data):
                self.api.side_effect = lambda *args, **kwargs: self.response(data)
                with self.assertRaises(RuntimeError):
                    resolver.resolve(BRANCH)

    @staticmethod
    def response(data):
        return io.StringIO(json.dumps(data))

    def test_both_transports_fail_with_bounded_retries(self):
        self.git.side_effect = subprocess.TimeoutExpired("git", 45)
        self.api.side_effect = URLError("unavailable")
        with self.assertRaisesRegex(RuntimeError, "refusing stale pins"):
            resolver.resolve(BRANCH)
        self.assertEqual(self.git.call_count, 3)
        self.assertEqual(self.api.call_count, 3)
        self.assertEqual([call.args[0] for call in self.sleep.call_args_list], [5, 10])

    def test_invalid_branch_never_starts_network_requests(self):
        with self.assertRaises(ValueError):
            resolver.resolve("--upload-pack=other")
        self.git.assert_not_called()
        self.api.assert_not_called()


class WorkflowTests(unittest.TestCase):
    def run_resolution(self, mode="latest", selection="6.1.x-android14", enabled="true", event="workflow_dispatch", fail=False):
        workflow = yaml.safe_load((ROOT / ".github/workflows/main.yml").read_text(encoding="utf-8"))
        steps = workflow["jobs"]["resolve-sources"]["steps"]
        step = next(step for step in steps if step.get("id") == "resolve")
        self.assertTrue(any(step.get("uses") == "actions/checkout@v7" for step in steps[:steps.index(step)]))
        values = {
            "inputs.commit_mode || 'latest'": mode,
            "github.event_name": event,
            "inputs.use_susfs": enabled,
            "inputs.root_flavor || 'All (KernelSU-Next + KernelSU + ReSukiSU)'": "KernelSU-Next",
            "inputs.root_flavor": "KernelSU-Next",
            "inputs.kernel_build_version || 'All'": selection,
        }
        script = re.sub(r"\$\{\{\s*(.*?)\s*\}\}", lambda match: values[match[1]], step["run"])
        # Execute the actual workflow shell, replacing only network commands.
        mocks = f"""
git() {{ printf '%s\\t%s\\n' '{SHA}' "${{@: -1}}"; }}
python3() {{ printf '%s\\n' "$2" >> "$LOOKUPS"; [ "$FAIL_LOOKUP" = false ] || return 1; printf '%s\\n' '{SHA}'; }}
"""
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "output"
            lookups = Path(directory) / "lookups"
            shell_file = Path(directory) / "resolve.sh"
            shell_file.write_text(mocks + script, encoding="utf-8", newline="\n")
            env = dict(os.environ, GITHUB_OUTPUT=output.as_posix(), LOOKUPS=lookups.as_posix(), FAIL_LOOKUP=str(fail).lower())
            result = subprocess.run([shutil.which("bash"), shell_file.as_posix()], env=env, capture_output=True, text=True, timeout=15)
            outputs = dict(line.split("=", 1) for line in output.read_text().splitlines()) if output.exists() else {}
            branches = lookups.read_text().splitlines() if lookups.exists() else []
        return result, {key: value for key, value in outputs.items() if key.startswith("susfs_commit_")}, branches

    def test_latest_and_update_resolve_selected_branch_once_for_all_roots(self):
        for mode in ["latest", "update"]:
            with self.subTest(mode=mode):
                result, outputs, branches = self.run_resolution(mode=mode)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(branches, [BRANCH])
                self.assertEqual(len(outputs), 3)
                self.assertEqual(set(outputs.values()), {SHA})

    def test_all_and_schedule_resolve_each_branch_once(self):
        for event in ["workflow_dispatch", "schedule"]:
            with self.subTest(event=event):
                result, outputs, branches = self.run_resolution(selection="All", event=event)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(len(branches), 7)
                self.assertEqual(len(set(branches)), 7)
                self.assertEqual(len(outputs), 21)

    def test_each_kernel_selection_resolves_its_own_branch(self):
        for version in ["android12-5.10", "android13-5.10", "android13-5.15", "android14-5.15", "android15-6.6", "android16-6.12"]:
            android, kernel = version.split("-")
            with self.subTest(version=version):
                result, outputs, branches = self.run_resolution(selection=f"{kernel}.x-{android}")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(branches, [f"gki-{version}"])
                self.assertEqual(len(outputs), 3)

    def test_verified_uses_pins_without_latest_lookup(self):
        result, outputs, branches = self.run_resolution(mode="verified")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(branches, [])
        self.assertEqual(len(outputs), 3)
        self.assertNotIn(SHA, outputs.values())

    def test_disabled_susfs_makes_no_susfs_requests(self):
        result, outputs, branches = self.run_resolution(enabled="false")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((outputs, branches), ({}, []))

    def test_failed_lookup_stops_workflow_without_emitting_stale_sha(self):
        result, outputs, branches = self.run_resolution(fail=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(outputs, {})
        self.assertEqual(branches, [BRANCH])


if __name__ == "__main__":
    unittest.main()

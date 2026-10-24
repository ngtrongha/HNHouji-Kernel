import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import unittest
from unittest.mock import patch

REPO = Path(__file__).resolve().parents[3]

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

pack = load("paired_pack", REPO / "modules/houji-experience/tools/package.py")
release = load("paired_release", REPO / "scripts/collect-houji-experience.py")

class PairedReleaseTests(unittest.TestCase):
    def test_download_uses_run_repository_even_with_upstream_gh_default(self):
        with patch.dict(os.environ, {"GITHUB_REPOSITORY":"ngtrongha/HNHouji-Kernel", "GH_REPO":"WildKernels/GKI_KernelSU_SUSFS"}):
            with patch.object(release.subprocess, "run") as run:
                release.download_artifact(88, "exact-artifact", Path("destination"))
        args = run.call_args.args[0]
        self.assertIn("--repo", args)
        self.assertEqual(args[args.index("--repo")+1], "ngtrongha/HNHouji-Kernel")
        self.assertIn("exact-artifact", args)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.assets = self.root / "release-assets"
        self.assets.mkdir()
        self.original = pack.ROOT
        pack.ROOT = self.root / "module"
        self.addCleanup(setattr, pack, "ROOT", self.original)
        for name in ("module.prop", "skip_mount", "customize.sh", "service.sh", "uninstall.sh", "webroot/index.html", "webroot/app.js", "webroot/style.css", "licenses/GO-LICENSE.txt", "README.md"):
            path = pack.ROOT / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("test-only", encoding="utf-8")
        binary = pack.ROOT / "bin/houjictl"
        binary.parent.mkdir()
        header = bytearray(64)
        header[:5] = b"\x7fELF\x02"
        struct.pack_into("<H", header, 18, 183)
        binary.write_bytes(header)
        binary.with_name("build-info.txt").write_text("CGO_ENABLED=1 GOOS=android GOARCH=arm64")
        self.artifacts = {}
        self.requests = []

    def pair(self, root_flavor):
        name = f"6.1.177-android14-lts-{root_flavor}-AnyKernel3"
        raw = self.root / root_flavor
        raw.mkdir()
        image = raw / "Image"
        image.write_bytes(b"kernel-image-" + root_flavor.encode())
        kernel = {"schema":1,"device":"houji","module_contract":1,"kernel_release":"6.1.177-android14-r11","config_sha256":"a"*64,"build_id":"b"*40,"image_sha256":hashlib.sha256(image.read_bytes()).hexdigest(),"source_revision":"c"*40}
        manifest = raw / "houji-kernel.json"
        manifest.write_text(json.dumps(kernel))
        (raw / "anykernel.sh").write_text("# test-only")
        shutil.make_archive(str(self.assets / name), "zip", raw)
        module_dir = self.root / (root_flavor + "-module")
        target = pack.package(module_dir, manifest, name, image)
        self.artifacts[name] = raw
        self.artifacts[name + "-HoujiExperience"] = module_dir
        return name, target

    def download(self, run_id, name, destination):
        self.requests.append((run_id, name))
        shutil.copytree(self.artifacts[name], destination, dirs_exist_ok=True)

    def test_two_kernel_variants_keep_distinct_matching_modules(self):
        first, first_zip = self.pair("KernelSU-Next")
        second, second_zip = self.pair("ReSukiSU")
        count = release.collect(self.assets, 88, self.download, [first, second])
        self.assertEqual(count, 2)
        self.assertEqual(self.requests, [(88, first + "-HoujiExperience"), (88, second + "-HoujiExperience")])
        self.assertNotEqual(first_zip.name, second_zip.name)
        self.assertTrue((self.assets / first_zip.name).is_file())
        self.assertTrue((self.assets / second_zip.name).is_file())
        self.assertEqual(len(list(self.assets.glob("HNHouji-*.manifest.json"))), 2)

    def test_wrong_pair_aborts_before_any_module_asset_is_copied(self):
        first, _ = self.pair("KernelSU-Next")
        second, _ = self.pair("ReSukiSU")
        self.artifacts[second + "-HoujiExperience"] = self.artifacts[first + "-HoujiExperience"]
        with self.assertRaisesRegex(ValueError, "provenance"):
            release.collect(self.assets, 88, self.download, [first, second])
        self.assertEqual(list(self.assets.glob("HNHouji-*")), [])

    def test_single_flat_or_missed_kernel_is_refetched_by_exact_name(self):
        name, module = self.pair("KernelSU-Next")
        (self.assets / (name + ".zip")).unlink()
        # Flat extraction must not make us guess identity from a kernel banner.
        (self.assets / "Image").write_bytes(b"ambiguous-flat-file")
        self.assertEqual(release.collect(self.assets, 99, self.download, [name]), 1)
        self.assertEqual(self.requests, [(99, name), (99, name + "-HoujiExperience")])
        self.assertTrue((self.assets / (name + ".zip")).is_file())
        self.assertTrue((self.assets / module.name).is_file())

    def test_corrupt_module_checksum_rejected(self):
        name, module = self.pair("KernelSU-Next")
        module.write_bytes(module.read_bytes() + b"corruption")
        with self.assertRaisesRegex(ValueError, "checksum"):
            release.collect(self.assets, 88, self.download, [name])
        self.assertEqual(list(self.assets.glob("HNHouji-*")), [])

    def test_missing_module_artifact_stops_collection(self):
        name, _ = self.pair("KernelSU-Next")
        def missing(run_id, artifact, destination):
            raise subprocess.CalledProcessError(1, ["gh", "run", "download", str(run_id), artifact])
        with self.assertRaises(subprocess.CalledProcessError):
            release.collect(self.assets, 88, missing, [name])
        self.assertEqual(list(self.assets.glob("HNHouji-*")), [])

    def test_other_kernel_families_do_not_require_houji_module(self):
        self.assertEqual(release.collect(self.assets, 88, self.download, []), 0)
        self.assertEqual(self.requests, [])

if __name__ == "__main__":
    unittest.main()

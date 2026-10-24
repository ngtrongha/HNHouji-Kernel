import importlib.util
from pathlib import Path
import struct
import tempfile
import unittest
import zipfile
import hashlib
import json

spec = importlib.util.spec_from_file_location("pack", Path(__file__).parents[1] / "tools/package.py")
pack = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pack)

class PackageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.original = pack.ROOT
        pack.ROOT = Path(self.tmp.name)
        self.addCleanup(setattr, pack, "ROOT", self.original)
        for name in ("module.prop", "skip_mount", "customize.sh", "service.sh", "uninstall.sh", "webroot/index.html", "webroot/app.js", "webroot/style.css", "licenses/GO-LICENSE.txt", "README.md"):
            path = pack.ROOT / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("fixture", encoding="utf-8")
        binary = pack.ROOT / "bin/houjictl"
        binary.parent.mkdir()
        header = bytearray(64)
        header[:5] = b"\x7fELF\x02"
        struct.pack_into("<H", header, 18, 183)
        binary.write_bytes(header)
        (binary.parent / "build-info.txt").write_text("CGO_ENABLED=1 GOOS=android GOARCH=arm64", encoding="utf-8")
        (pack.ROOT / "ai.json").write_text('"NOT_FOR_THE_ZIP"', encoding="utf-8")

    def test_allowlist_permissions_checksum_and_reproducibility(self):
        target = pack.package(pack.ROOT / "dist")
        first = target.read_bytes()
        with zipfile.ZipFile(target) as archive:
            self.assertNotIn("ai.json", archive.namelist())
            self.assertNotIn("bin/build-info.txt", archive.namelist())
            self.assertEqual((archive.getinfo("bin/houjictl").external_attr >> 16) & 0o777, 0o755)
            self.assertEqual((archive.getinfo("webroot/index.html").external_attr >> 16) & 0o777, 0o644)
        self.assertTrue(target.with_suffix(".zip.sha256").read_text().startswith(hashlib.sha256(first).hexdigest()))
        manifest = json.loads(target.with_suffix(".manifest.json").read_text())
        self.assertTrue(manifest["build"]["cgo"])
        pack.package(pack.ROOT / "dist")
        self.assertEqual(first, target.read_bytes())

    def test_rejects_non_android_or_cgo_disabled_binary(self):
        (pack.ROOT / "bin/build-info.txt").write_text("CGO_ENABLED=0 GOOS=android GOARCH=arm64")
        with self.assertRaisesRegex(ValueError, "CGO_ENABLED=1"):
            pack.package(pack.ROOT / "dist")
        (pack.ROOT / "bin/houjictl").write_bytes(b"MZ" + b"\0" * 62)
        with self.assertRaisesRegex(ValueError, "arm64 ELF"):
            pack.package(pack.ROOT / "dist")

    def paired_fixture(self):
        image = pack.ROOT / "Image"
        image.write_bytes(b"actual-kernel-image-fixture")
        kernel = {"schema":1,"device":"houji","module_contract":1,"kernel_release":"6.1.177-android14-r11","config_sha256":"a"*64,"build_id":"b"*40,"image_sha256":hashlib.sha256(image.read_bytes()).hexdigest(),"source_revision":"c"*40}
        metadata = pack.ROOT / "houji-kernel.json"
        metadata.write_text(json.dumps(kernel), encoding="utf-8")
        return metadata, image, kernel

    def test_paired_zip_binds_exact_image_and_artifact(self):
        metadata, image, kernel = self.paired_fixture()
        artifact = "6.1.177-android14-lts-KernelSU-Next-AnyKernel3"
        target = pack.package(pack.ROOT / "dist", metadata, artifact, image)
        self.assertIn("for-6.1.177-android14-lts-KernelSU-Next", target.name)
        with zipfile.ZipFile(target) as archive:
            self.assertEqual(json.loads(archive.read("kernel.json")), kernel)
            self.assertNotIn("Image", archive.namelist())
        info = json.loads(target.with_suffix(".manifest.json").read_text())
        self.assertEqual(info["kernel_artifact"], artifact)
        self.assertEqual(info["kernel"], kernel)
        generic = pack.package(pack.ROOT / "dist")
        with zipfile.ZipFile(generic) as archive:
            self.assertNotIn("kernel.json", archive.namelist())

    def test_pairing_rejects_missing_identity_corrupt_image_and_wrong_device(self):
        metadata, image, kernel = self.paired_fixture()
        artifact = "6.1.177-android14-lts-KernelSU-Next-AnyKernel3"
        with self.assertRaisesRegex(ValueError, "Pairing requires"):
            pack.package(pack.ROOT / "dist", metadata, artifact)
        with self.assertRaisesRegex(ValueError, "artifact name"):
            pack.package(pack.ROOT / "dist", metadata, "../../wrong", image)
        image.write_bytes(b"different-kernel")
        with self.assertRaisesRegex(ValueError, "Image SHA-256"):
            pack.package(pack.ROOT / "dist", metadata, artifact, image)
        kernel["device"] = "other"
        metadata.write_text(json.dumps(kernel))
        with self.assertRaisesRegex(ValueError, "not for houji"):
            pack.package(pack.ROOT / "dist", metadata, artifact, image)
if __name__ == "__main__":
    unittest.main()

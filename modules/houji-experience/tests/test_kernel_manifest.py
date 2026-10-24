import gzip
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("kernel_manifest", REPO / "scripts/write-houji-kernel-manifest.py")
binding = importlib.util.module_from_spec(spec)
spec.loader.exec_module(binding)

CONFIG = b"CONFIG_HZ=250\nCONFIG_ZRAM=m\n"
BUILD_ID = bytes.fromhex("ab" * 20)

def elf(notes=None):
    note = notes if notes is not None else struct.pack("<III", 4, len(BUILD_ID), 3) + b"GNU\0" + BUILD_ID
    data = bytearray(128 + len(note))
    data[:6] = b"\x7fELF\x02\x01"
    struct.pack_into("<Q", data, 40, 64)
    struct.pack_into("<HH", data, 58, 64, 1)
    struct.pack_into("<I", data, 68, 7)  # SHT_NOTE
    struct.pack_into("<QQ", data, 88, 128, len(note))
    data[128:] = note
    return data

def image():
    return b"Linux version 6.1.177-android14-r11 (builder)\0" + b"IKCFG_ST" + gzip.compress(CONFIG) + b"IKCFG_ED" + BUILD_ID

class KernelManifestTests(unittest.TestCase):
    def test_cli_reads_mmap_files_and_rejects_a_real_mismatch(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            raw, binary, output = root / "Image", root / "vmlinux", root / "kernel.json"
            raw.write_bytes(image())
            binary.write_bytes(elf())
            command = [sys.executable, str(REPO / "scripts/write-houji-kernel-manifest.py"),
                       "--image", str(raw), "--vmlinux", str(binary),
                       "--source-revision", "f" * 40, "--output", str(output)]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            metadata = json.loads(output.read_text(encoding="utf-8"))
            self.assertEqual(metadata["build_id"], BUILD_ID.hex())
            self.assertEqual(metadata["image_sha256"], hashlib.sha256(raw.read_bytes()).hexdigest())
            raw.write_bytes(image().replace(BUILD_ID, b"x" * 20))
            invalid_output = root / "invalid.json"
            command[-1] = str(invalid_output)
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("build ID is not present", result.stderr)
            self.assertFalse(invalid_output.exists())

    def test_exact_config_image_and_main_vmlinux_id(self):
        # Raw Images can also contain KVM IDs; main vmlinux decides identity.
        raw = image() + b"GNU\0" + b"other-kvm-id"
        m = binding.manifest(raw, elf(), "f" * 40)
        self.assertEqual(m["kernel_release"], "6.1.177-android14-r11")
        self.assertEqual(m["build_id"], BUILD_ID.hex())
        self.assertEqual(m["config_sha256"], hashlib.sha256(CONFIG).hexdigest())
        self.assertEqual(m["image_sha256"], hashlib.sha256(raw).hexdigest())

    def test_missing_corrupt_or_mixed_artifacts_rejected(self):
        for raw, binary, rev in [
            (image().replace(BUILD_ID, b"x" * 20), elf(), "f" * 40),
            (image().replace(b"IKCFG_ED", b"BROKEN__"), elf(), "f" * 40),
            (image(), elf(), "unknown"),
            (image() + b"Linux version 6.1.177-android14-other (other)", elf(), "f" * 40),
            (image(), b"not-an-elf", "f" * 40),
            (image(), elf(struct.pack("<III", 4, 1000, 3) + b"GNU\0"), "f" * 40),
        ]:
            with self.subTest(rev=rev, size=len(raw)):
                with self.assertRaises(ValueError):
                    binding.manifest(raw, binary, rev)

    def test_installer_wires_metadata_and_skips_conflicting_helper(self):
        bash = "C:/Program Files/Git/bin/bash.exe" if os.name == "nt" else shutil.which("bash")
        if not bash:
            self.skipTest("bash is required")
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "anykernel.sh").write_text('#!/bin/sh\nproperties() { echo "kernel.string=Original"; }\n', encoding="utf-8")
            result = subprocess.run([bash, (REPO / "scripts/customize-anykernel3.sh").as_posix(), root.as_posix(), "6.1"], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            generated = (root / "anykernel.sh").read_text()
            self.assertIn("/data/adb/houji-kernel/manifest.json.tmp", generated)
            self.assertIn("/data/adb/modules/houji_experience", generated)
            self.assertIn("legacy helper skipped", generated)
            checked = subprocess.run([bash, "-n", (root / "anykernel.sh").as_posix()], capture_output=True, text=True)
            self.assertEqual(checked.returncode, 0, checked.stderr)

if __name__ == "__main__":
    unittest.main()

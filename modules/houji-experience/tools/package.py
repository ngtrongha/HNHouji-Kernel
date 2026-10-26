"""Package an already built Android binary + WebUI, never developer state."""
from pathlib import Path
import argparse
import hashlib
import json
import struct
import zipfile
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from kernel_binding import file_sha256, validate_artifact, validate_kernel

ROOT = Path(__file__).resolve().parents[1]

def package(output: Path, kernel_manifest: Path | None = None, kernel_artifact: str | None = None, kernel_image: Path | None = None) -> Path:
    pairing = (kernel_manifest is not None, kernel_artifact is not None, kernel_image is not None)
    if any(pairing) and not all(pairing):
        raise ValueError("Pairing requires manifest, exact artifact name and kernel Image")
    kernel = None
    if kernel_manifest is not None:
        if kernel_manifest.stat().st_size > 256 * 1024:
            raise ValueError("Kernel manifest too large")
        kernel = validate_kernel(json.loads(kernel_manifest.read_text(encoding="utf-8")))
        validate_artifact(kernel_artifact)
        if file_sha256(kernel_image) != kernel["image_sha256"]:
            raise ValueError("Kernel Image SHA-256 does not match build metadata")
    binary = ROOT / "bin/houjictl"
    header = binary.read_bytes()[:64]
    if header[:4] != b"\x7fELF" or header[4] != 2 or struct.unpack_from("<H", header, 18)[0] != 183:
        raise ValueError("bin/houjictl must be an arm64 ELF, not a host executable")
    if not (ROOT / "webroot/app.js").is_file():
        raise ValueError("Build the WebUI first")
    build_info = (ROOT / "bin/build-info.txt").read_text(encoding="utf-8-sig")
    for required in ("CGO_ENABLED=1", "GOOS=android", "GOARCH=arm64"):
        if required not in build_info:
            raise ValueError(f"Android build metadata missing {required}; Bionic DNS is required")
    files = [ROOT / n for n in ("module.prop", "skip_mount", "customize.sh", "service.sh", "uninstall.sh", "bin/houjictl")]
    files += sorted((ROOT / "webroot").glob("*"))
    files += sorted((ROOT / "licenses").glob("*"))
    files += [ROOT / "README.md"]
    output.mkdir(parents=True, exist_ok=True)
    suffix = ""
    if kernel is not None:
        suffix = f"-for-{kernel_artifact.removesuffix('-AnyKernel3')}-{kernel['image_sha256'][:12]}"
    target = output / f"HNHouji-Experience-0.6.0-preview{suffix}.zip"
    checksums = {}
    with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        payloads = [(path.relative_to(ROOT).as_posix(), path.read_bytes()) for path in files if path.is_file()]
        for path in files:
            if not path.is_file():
                raise ValueError(f"Missing package input: {path.name}")
        if kernel is not None:
            payloads.append(("kernel.json", (json.dumps(kernel, indent=2) + "\n").encode("utf-8")))
        for name, data in payloads:
            info = zipfile.ZipInfo(name, date_time=(2026, 10, 24, 0, 0, 0))
            info.create_system = 3
            mode = 0o755 if name.endswith(".sh") or name == "bin/houjictl" else 0o644
            info.external_attr = (0o100000 | mode) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, data)
            checksums[name] = hashlib.sha256(data).hexdigest()
    digest = hashlib.sha256(target.read_bytes()).hexdigest()
    target.with_suffix(".zip.sha256").write_text(f"{digest}  {target.name}\n", encoding="utf-8")
    target.with_suffix(".manifest.json").write_text(json.dumps({"status": "preview-device-validation-required", "kernel_artifact": kernel_artifact, "kernel": kernel, "build": {"target": "android/arm64", "cgo": True, "ndk": "28.1.13356709"}, "files": checksums}, indent=2) + "\n", encoding="utf-8")
    return target

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    parser.add_argument("--kernel-manifest", type=Path)
    parser.add_argument("--kernel-artifact")
    parser.add_argument("--kernel-image", type=Path)
    args = parser.parse_args()
    print(package(args.output.resolve(), args.kernel_manifest, args.kernel_artifact, args.kernel_image))

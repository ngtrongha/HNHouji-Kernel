"""Collect and verify same-run paired modules before publishing kernel assets.

Each kernel ZIP determines the exact module artifact name. No latest-release
lookup, broad artifact pattern, or source-tree extraction is involved.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "modules/houji-experience/tools"))
from kernel_binding import file_sha256, validate_artifact, validate_kernel

def read_json(path):
    if path.stat().st_size > 256 * 1024:
        raise ValueError(f"Oversized metadata: {path.name}")
    return json.loads(path.read_text(encoding="utf-8"))

def zip_json(archive, name):
    if archive.getinfo(name).file_size > 256 * 1024:
        raise ValueError(f"Oversized ZIP metadata: {name}")
    return json.loads(archive.read(name))

def download_artifact(run_id, name, destination):
    subprocess.run(["gh", "run", "download", str(run_id), "--name", name, "--dir", str(destination)], check=True)

def list_kernel_artifacts(run_id):
    repository = os.environ["GITHUB_REPOSITORY"]
    names = []
    for page in range(1, 51):
        result = subprocess.run(["gh", "api", f"repos/{repository}/actions/runs/{run_id}/artifacts?per_page=100&page={page}"], capture_output=True, text=True, check=True)
        artifacts = json.loads(result.stdout)["artifacts"]
        for artifact in artifacts:
            name = artifact["name"]
            if not artifact["expired"] and name.startswith("6.1.") and "-android14-" in name and name.endswith("-AnyKernel3"):
                names.append(validate_artifact(name))
        if len(artifacts) < 100:
            return sorted(set(names))
    raise ValueError("Artifact listing exceeded 50 pages; refusing incomplete publication")

def verify_module(directory, kernel, artifact):
    zips = list(directory.glob("*.zip"))
    if len(zips) != 1:
        raise ValueError(f"Expected one module ZIP for {artifact}, found {len(zips)}")
    module = zips[0]
    checksum = module.with_suffix(".zip.sha256")
    metadata = module.with_suffix(".manifest.json")
    fields = checksum.read_text(encoding="utf-8").split()
    if fields != [file_sha256(module), module.name]:
        raise ValueError(f"Module checksum mismatch: {module.name}")
    info = read_json(metadata)
    if info.get("kernel_artifact") != artifact or validate_kernel(info.get("kernel")) != kernel:
        raise ValueError(f"Module provenance does not match {artifact}")
    with zipfile.ZipFile(module) as archive:
        if validate_kernel(zip_json(archive, "kernel.json")) != kernel:
            raise ValueError(f"Embedded module binding does not match {artifact}")
        files = info.get("files")
        if not isinstance(files, dict) or set(files) != set(archive.namelist()) or len(files) != len(archive.namelist()):
            raise ValueError(f"Module file manifest mismatch: {module.name}")
        for name, sha in files.items():
            if archive.getinfo(name).file_size > 32 * 1024 * 1024:
                raise ValueError(f"Oversized module entry: {name}")
            if hashlib.sha256(archive.read(name)).hexdigest() != sha:
                raise ValueError(f"Module entry checksum mismatch: {name}")
    return module, checksum, metadata

def collect(assets, run_id, downloader=download_artifact, kernel_artifacts=None):
    assets.mkdir(parents=True, exist_ok=True)
    expected = list_kernel_artifacts(run_id) if kernel_artifacts is None else kernel_artifacts
    kernels = [assets / (validate_artifact(name) + ".zip") for name in expected]
    pending = []
    # Validate every pair before copying any output to the release root.
    with tempfile.TemporaryDirectory(prefix="houji-release-") as temporary:
        for index, path in enumerate(kernels):
            artifact = validate_artifact(path.stem)
            # Recover server-side pattern misses and single-artifact flat
            # extraction using this run's exact artifact identity.
            if not path.is_file():
                raw = Path(temporary) / f"kernel-{index}"
                raw.mkdir()
                downloader(run_id, artifact, raw)
                with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
                    for source in sorted(raw.rglob("*")):
                        if source.is_symlink():
                            raise ValueError("Unexpected symlink in kernel artifact")
                        if source.is_file():
                            archive.write(source, source.relative_to(raw).as_posix())
            with zipfile.ZipFile(path) as archive:
                kernel = validate_kernel(zip_json(archive, "houji-kernel.json"))
                digest = hashlib.sha256()
                with archive.open("Image") as stream:
                    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                        digest.update(chunk)
                if digest.hexdigest() != kernel["image_sha256"]:
                    raise ValueError(f"Kernel Image/metadata mismatch: {artifact}")
            destination = Path(temporary) / str(index)
            destination.mkdir()
            downloader(run_id, artifact + "-HoujiExperience", destination)
            pending.extend(verify_module(destination, kernel, artifact))
        names = [path.name for path in pending]
        if len(names) != len(set(names)):
            raise ValueError("Paired module asset names collide")
        for path in pending:
            shutil.copyfile(path, assets / path.name)
    print(f"Verified {len(kernels)} kernel/module pairs from run {run_id}")
    return len(kernels)

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--run-id", type=int, required=True)
    args = parser.parse_args()
    if args.run_id <= 0:
        parser.error("run-id must be positive")
    collect(args.assets.resolve(), args.run_id)

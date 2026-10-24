"""Shared validation for paired module packaging and release collection."""
import re
import hashlib

def file_sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()

def validate_kernel(value):
    if not isinstance(value, dict):
        raise ValueError("Kernel manifest must be an object")
    for key in ("schema", "module_contract"):
        if type(value.get(key)) is not int or value[key] != 1:
            raise ValueError(f"Unsupported kernel {key}")
    if value.get("device") != "houji":
        raise ValueError("Kernel manifest is not for houji")
    patterns = {
        "kernel_release": r"6\.1\.[0-9]+-android14-[A-Za-z0-9_.+\-]{1,160}",
        "config_sha256": r"[0-9a-f]{64}",
        "image_sha256": r"[0-9a-f]{64}",
        "build_id": r"(?:[0-9a-f]{2}){8,64}",
        "source_revision": r"[0-9a-f]{40}",
    }
    for key, pattern in patterns.items():
        if not isinstance(value.get(key), str) or not re.fullmatch(pattern, value[key]):
            raise ValueError(f"Invalid kernel {key}")
    return {key: value[key] for key in ("schema", "device", "module_contract", *patterns)}

def validate_artifact(name):
    if not isinstance(name, str) or len(name) > 160 or not re.fullmatch(r"6\.1\.[A-Za-z0-9_.+\-]+-android14-[A-Za-z0-9_.+\-]+-AnyKernel3", name):
        raise ValueError("Invalid houji kernel artifact name")
    return name

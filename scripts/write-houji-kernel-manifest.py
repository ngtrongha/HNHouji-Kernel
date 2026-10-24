"""Bind Houji Experience to the Image, embedded IKCONFIG and vmlinux build ID.

Read-only with respect to kernel source/config: writes one packaging sidecar.
"""
import argparse
import hashlib
import json
import mmap
from pathlib import Path
import re
import struct
import zlib

def build_id_from_elf(data):
    if data[:6] != b"\x7fELF\x02\x01" or len(data) < 64:
        raise ValueError("Expected little-endian ELF64 vmlinux")
    offset = struct.unpack_from("<Q", data, 40)[0]
    width, count = struct.unpack_from("<HH", data, 58)
    if width < 64 or count == 0 or offset + width * count > len(data):
        raise ValueError("Invalid ELF section table")
    ids = set()
    for i in range(count):
        section = offset + i * width
        if struct.unpack_from("<I", data, section + 4)[0] != 7:
            continue
        start, size = struct.unpack_from("<QQ", data, section + 24)
        end = start + size
        if end > len(data):
            raise ValueError("Invalid ELF note bounds")
        while start + 12 <= end:
            n, d, kind = struct.unpack_from("<III", data, start)
            name = start + 12
            desc = name + ((n + 3) & ~3)
            next_note = desc + ((d + 3) & ~3)
            if next_note > end:
                raise ValueError("Invalid ELF note")
            if kind == 3 and data[name:name+n] == b"GNU\0" and 8 <= d <= 64:
                ids.add(data[desc:desc+d].hex())
            start = next_note
    if len(ids) != 1:
        raise ValueError("vmlinux must expose one unambiguous GNU build ID")
    return ids.pop()

def embedded_config(image):
    pos = 0
    while True:
        pos = image.find(b"IKCFG_ST", pos)
        if pos < 0:
            raise ValueError("Image has no usable embedded IKCONFIG")
        start = pos + 8
        pos = start
        if image[start:start+3] != b"\x1f\x8b\x08":
            continue
        try:
            decoder = zlib.decompressobj(31)
            data = decoder.decompress(image[start:start+2*1024*1024], 2*1024*1024+1)
            if len(data) <= 2*1024*1024 and decoder.eof and decoder.unused_data.startswith(b"IKCFG_ED"):
                return data
        except zlib.error:
            continue

def manifest(image, vmlinux, revision):
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("Provide the actual 40-character source revision")
    releases = set(re.findall(rb"Linux version (6\.1\.[0-9]+-android14-[!-~]+) ", image))
    if len(releases) != 1:
        raise ValueError("Image must contain one Android14 6.1 release")
    build_id = build_id_from_elf(vmlinux)
    # mmap iteration yields individual bytes; `needle in mmap` does not perform
    # a multi-byte substring search. find() works for both mmap and bytes.
    if image.find(bytes.fromhex(build_id)) < 0:
        raise ValueError("vmlinux build ID is not present in this Image")
    return {"schema": 1, "device": "houji", "module_contract": 1,
            "kernel_release": releases.pop().decode("ascii"),
            "config_sha256": hashlib.sha256(embedded_config(image)).hexdigest(),
            "build_id": build_id, "image_sha256": hashlib.sha256(image).hexdigest(),
            "source_revision": revision,
            "verification": "artifact identity; not a device boot or KMI certification"}

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--image", type=Path, required=True)
    parser.add_argument("--vmlinux", type=Path, required=True)
    parser.add_argument("--source-revision", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    with args.image.open("rb") as image_file, args.vmlinux.open("rb") as elf_file:
        with mmap.mmap(image_file.fileno(), 0, access=mmap.ACCESS_READ) as image:
            with mmap.mmap(elf_file.fileno(), 0, access=mmap.ACCESS_READ) as elf:
                result = manifest(image, elf, args.source_revision)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")

"""Align only namespace.c's vendor-hook include with the selected SUSFS patch."""
import argparse
from pathlib import Path

HEADER = "#include <trace/hooks/blk.h>\n"
ANCHOR = '#include "internal.h"\n'

def restore(source):
    count = source.splitlines(keepends=True).count(HEADER)
    if count == 1:
        return source
    if count != 0 or source.count(ANCHOR) != 1:
        raise ValueError("Ambiguous namespace vendor-hook include or insertion anchor")
    return source.replace(ANCHOR, ANCHOR + HEADER, 1)

def prepare(source, patch):
    sections = [part for part in patch.split("diff --git ") if part.startswith("a/fs/namespace.c b/fs/namespace.c\n")]
    if len(sections) != 1 or "\n@@ " not in sections[0]:
        raise ValueError("Cannot identify namespace.c context in selected SUSFS patch")
    if " " + HEADER in sections[0].splitlines(keepends=True):
        return restore(source)
    return "".join(line for line in source.splitlines(keepends=True) if line != HEADER)

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=["apply", "restore"])
    parser.add_argument("namespace", type=Path)
    parser.add_argument("patch", type=Path, nargs="?")
    args = parser.parse_args()
    source = args.namespace.read_text(encoding="utf-8")
    if args.mode == "apply":
        if args.patch is None:
            parser.error("apply requires the selected SUSFS patch")
        result = prepare(source, args.patch.read_text(encoding="utf-8"))
    else:
        result = restore(source)
    if result != source:
        args.namespace.write_text(result, encoding="utf-8")
    print(f"namespace.c vendor-hook include: {args.mode} complete")

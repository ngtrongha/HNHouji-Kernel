#!/usr/bin/env python3
"""Allow unused SUSFS credential helpers in older BakaSU source combinations."""

from pathlib import Path
import re
import sys


def mark_optional_helpers(source):
    # Callers can be enabled by either path filtering or priority UID filtering.
    return re.sub(r"(?m)^static (?=(?:struct cred \*susfs_switch_to_ksu_domain|"
                  r"void susfs_restore_creds)\()", "static __maybe_unused ", source)


if __name__ == "__main__":
    path = Path(sys.argv[1])
    original = path.read_text(encoding="utf-8")
    updated = mark_optional_helpers(original)
    if updated != original:
        path.write_text(updated, encoding="utf-8")

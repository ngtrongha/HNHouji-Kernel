#!/usr/bin/env python3
"""Guard the SUSFS credential helpers used only by priority UID filtering."""

from pathlib import Path
import re
import sys


def guard_helpers(source):
    # Keep the helper definitions under the same option as their call sites.
    guard = "#ifdef CONFIG_KSU_SUSFS_PRIORITY_UID_FILTER\n"
    end = "#endif /* SUSFS priority UID credential helpers */\n"
    if end in source or "static struct cred *susfs_switch_to_ksu_domain(" not in source:
        return source

    pattern = (
        r"(?ms)^/\*\n \* Switches the selinux domain of the current process\b"
        r".*?^static struct cred \*susfs_switch_to_ksu_domain\(void\) \{\n.*?^\}\n"
        r"\n/\*\n \* Restores the original credentials\b"
        r".*?^static void susfs_restore_creds\(struct cred \*orig_cred\) \{\n.*?^\}\n"
    )
    matches = list(re.finditer(pattern, source))
    if len(matches) != 1:
        raise ValueError("unexpected SUSFS credential helper layout")
    match = matches[0]
    return source[:match.start()] + guard + match[0] + end + source[match.end():]


if __name__ == "__main__":
    path = Path(sys.argv[1])
    original = path.read_text(encoding="utf-8")
    updated = guard_helpers(original)
    if updated != original:
        path.write_text(updated, encoding="utf-8")

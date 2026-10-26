#!/usr/bin/env python3
"""Resolve an official SUSFS branch tip, with GitLab REST as a Git fallback."""

import json
import re
import subprocess
import sys
import time
from urllib.error import URLError
from urllib.request import urlopen

REPO = "https://gitlab.com/simonpunk/susfs4ksu.git"
API = "https://gitlab.com/api/v4/projects/simonpunk%2Fsusfs4ksu/repository/branches/"


def resolve(branch):
    if not re.fullmatch(r"gki-android[0-9]+-[0-9]+\.[0-9]+", branch):
        raise ValueError("Invalid SUSFS branch")
    ref = f"refs/heads/{branch}"
    for attempt in range(3):
        try:
            result = subprocess.run(
                ["git", "ls-remote", "--exit-code", REPO, ref],
                check=True, capture_output=True, text=True, timeout=45,
            )
            fields = result.stdout.split()
            if len(fields) != 2 or fields[1] != ref or not re.fullmatch(r"[0-9a-f]{40}", fields[0]):
                raise ValueError("Git returned an invalid branch tip")
            return fields[0]
        except (OSError, subprocess.SubprocessError, ValueError) as error:
            print(f"SUSFS Git lookup failed for {branch}: {error}; trying official GitLab API", file=sys.stderr)

        try:
            with urlopen(API + branch, timeout=30) as response:
                data = json.load(response)
            sha = data["commit"]["id"]
            if data["name"] != branch or not isinstance(sha, str) or not re.fullmatch(r"[0-9a-f]{40}", sha):
                raise ValueError("GitLab API returned an invalid branch tip")
            print(f"Resolved {branch} via official GitLab API: {sha}", file=sys.stderr)
            return sha
        except (OSError, URLError, ValueError, KeyError, TypeError) as error:
            print(f"SUSFS API lookup failed for {branch}: {error}", file=sys.stderr)

        if attempt < 2:
            time.sleep(5 * (2 ** attempt))
    raise RuntimeError(f"Cannot resolve latest SUSFS for {branch} via Git or API; refusing stale pins")


if __name__ == "__main__":
    try:
        if len(sys.argv) != 2:
            raise ValueError("Usage: resolve_susfs.py gki-android14-6.1")
        print(resolve(sys.argv[1]))
    except (ValueError, RuntimeError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)

"""Collect the license notices for code actually redistributed in the binary."""
from pathlib import Path
import shutil
import subprocess

root = Path(__file__).resolve().parents[1]
destination = root / "licenses"
destination.mkdir(exist_ok=True)
shutil.copyfile(root.parents[1] / "LICENSE", destination / "PROJECT-LICENSE.txt")
goroot = Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
shutil.copyfile(goroot / "LICENSE", destination / "GO-LICENSE.txt")
for notice in (goroot / "src/vendor").rglob("LICENSE"):
    name = "GO-VENDOR-" + "-".join(notice.relative_to(goroot / "src/vendor").parts[:-1]) + ".txt"
    shutil.copyfile(notice, destination / name)

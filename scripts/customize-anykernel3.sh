#!/usr/bin/env bash
set -euo pipefail

AK3_DIR="${1:-./AnyKernel3}"
KERNEL_VERSION="${2:-6.1}"

if [ ! -d "$AK3_DIR" ]; then
  echo "[-] AnyKernel3 directory not found at $AK3_DIR, skipping customization."
  exit 0
fi

if [ "$KERNEL_VERSION" = "6.1" ]; then
  KERNEL_NAME="HNHouji Kernel for Xiaomi 14 (houji)"
else
  KERNEL_NAME="HNHouji GKI Kernel ($KERNEL_VERSION)"
fi

echo "[+] Customizing AnyKernel3 for $KERNEL_NAME..."

# 1. Replace ASCII art banner
cat << 'EOF' > "$AK3_DIR/banner"
 _    _ _   _ _    _             _ _ 
| |  | | \ | | |  | |           (_|_)
| |__| |  \| | |__| | ___  _   _ _ _ 
|  __  | . ` |  __  |/ _ \| | | | | |
| |  | | |\  | |  | | (_) | |_| | | |
|_|  |_|_| \_|_|  |_|\___/ \__,_| |_|
EOF

# 2. Re-brand anykernel.sh
if [ -f "$AK3_DIR/anykernel.sh" ]; then
  python3 - << PYEOF
import os

ak3_file = os.path.join("$AK3_DIR", "anykernel.sh")
with open(ak3_file, "r") as f:
    lines = f.readlines()

new_lines = []
for line in lines:
    if line.startswith("kernel.string="):
        new_lines.append("kernel.string=${KERNEL_NAME}\n")
    elif "Wild Kernels Supported:" in line:
        new_lines.append(line.replace("Wild Kernels Supported:", "HNHouji GKI Supported:"))
    elif "flash_boot" in line:
        new_lines.append(line)
    elif line.strip() == "fi":
        new_lines.append(line)
        break
    else:
        new_lines.append(line)

with open(ak3_file, "w") as f:
    f.writelines(new_lines)
PYEOF

  # 3. Append clean UI prints and device verification
  if [ "$KERNEL_VERSION" = "6.1" ]; then
    cat << 'EOF' >> "$AK3_DIR/anykernel.sh"

ui_print " "
ui_print "============================================"
ui_print "       HNHouji Kernel for Xiaomi 14         "
ui_print "============================================"
ui_print "  * Base: Google ACK 6.1 LTS"
ui_print "  * Upstream: WildKernels by TheWildJames"
ui_print "  * Installer: AnyKernel3 by osm0sis"
ui_print "  * Display DRM & SPMI: Stock 6.1.157 Shield"
ui_print "  * Root & Hide: KernelSU-Next + SUSFS v2.3.0"
ui_print "  * Addons: BBRv3, Baseband Guard, DroidSpaces"
ui_print "  * Perf: ARM64 NEON/Prefetch + S2Idle + Default FQ"
ui_print "  * New stock shield: device validation pending"
ui_print "============================================"
ui_print " "
EOF
  else
    cat << 'EOF' >> "$AK3_DIR/anykernel.sh"

ui_print " "
ui_print "============================================"
ui_print "            HNHouji GKI Kernel              "
ui_print "============================================"
ui_print "  * Base: Google ACK GKI 2.0"
ui_print "  * Upstream: WildKernels by TheWildJames"
ui_print "  * Installer: AnyKernel3 by osm0sis"
ui_print "  * Features: Root & Kernel Hardening"
ui_print "============================================"
ui_print " "
EOF
  fi

  # 4. Retire the previous helper when recovery can access /data.
  if [ "$KERNEL_VERSION" = "6.1" ]; then
    cat << 'EOF' >> "$AK3_DIR/anykernel.sh"

# Keep the backup outside service.d, where executable files can run at boot.
houji_helper=/data/adb/service.d/houji-notification-helper.sh
houji_backup=/data/adb/houji-kernel/houji-notification-helper.sh.disabled
if [ -f "$houji_helper" ]; then
  if mkdir -p /data/adb/houji-kernel && [ ! -e "$houji_backup" ] && [ ! -L "$houji_backup" ] &&
     mv "$houji_helper" "$houji_backup"; then
    ui_print "  -> Previous Houji helper moved out of service.d."
  else
    ui_print "  -> Previous helper still present. Use Houji Experience to retire it."
  fi
fi
EOF
    echo "[+] Added recovery-time retirement for the previous Houji helper"
  fi

  if [ "$KERNEL_VERSION" = "6.1" ]; then
    cat << 'EOF' >> "$AK3_DIR/anykernel.sh"

# Publish build identity only; this does not modify the kernel or its config.
if [ -f "$home/houji-kernel.json" ]; then
  if mkdir -p /data/adb/houji-kernel && cp "$home/houji-kernel.json" /data/adb/houji-kernel/manifest.json.tmp; then
    chmod 600 /data/adb/houji-kernel/manifest.json.tmp
    if mv /data/adb/houji-kernel/manifest.json.tmp /data/adb/houji-kernel/manifest.json; then
      ui_print "  -> Houji kernel build metadata installed."
    else
      ui_print "  -> Metadata unavailable: module will remain diagnostic-only."
    fi
  else
    ui_print "  -> Cannot write /data/adb metadata; module will remain diagnostic-only."
  fi
fi
EOF
  fi

  cat << 'EOF' >> "$AK3_DIR/anykernel.sh"

ui_print " "
ui_print "Flashing completed! Reboot and enjoy HNHouji Kernel."
ui_print " "
EOF
fi

echo "[+] Successfully configured AnyKernel3 flasher for $KERNEL_NAME"

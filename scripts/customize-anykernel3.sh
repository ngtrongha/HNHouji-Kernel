#!/usr/bin/env bash
set -euo pipefail

AK3_DIR="${1:-./AnyKernel3}"
KERNEL_VERSION="${2:-6.1}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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
ui_print "  * Display DRM & SPMI: Stock 6.1.138 Shield"
ui_print "  * Root & Hide: KernelSU-Next + SUSFS v2.3.0"
ui_print "  * Addons: BBRv3, Baseband Guard, DroidSpaces"
ui_print "  * Perf: ARM64 NEON/Prefetch + S2Idle + Default FQ"
ui_print "  * Safety: 0-BlackScreen, 0-Flicker, Clean F2FS"
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

  # 4. Wire Xiaomi 14 notification keeper if 6.1
  HELPER_SRC="$SCRIPT_DIR/houji-notification-helper.sh"
  if [ "$KERNEL_VERSION" = "6.1" ] && [ -f "$HELPER_SRC" ]; then
    tr -d '\r' < "$HELPER_SRC" > "$AK3_DIR/houji-notification-helper.sh"
    chmod +x "$AK3_DIR/houji-notification-helper.sh" || true
    cat << 'EOF' >> "$AK3_DIR/anykernel.sh"

# Xiaomi 14 (houji) notification & performance helper auto-install
if [ -f "$home/houji-notification-helper.sh" ]; then
  ui_print "  -> Installing Xiaomi 14 performance & notification helper..."
  mkdir -p /data/adb/service.d
  tr -d '\r' < "$home/houji-notification-helper.sh" > /data/adb/service.d/houji-notification-helper.sh
  chmod 755 /data/adb/service.d/houji-notification-helper.sh
  ui_print "  -> Performance & notification helper installed successfully."
fi
EOF
    echo "[+] Wired houji-notification-helper.sh into AnyKernel3"
  fi

  cat << 'EOF' >> "$AK3_DIR/anykernel.sh"

ui_print " "
ui_print "Flashing completed! Reboot and enjoy HNHouji Kernel."
ui_print " "
EOF
fi

echo "[+] Successfully configured AnyKernel3 flasher for $KERNEL_NAME"

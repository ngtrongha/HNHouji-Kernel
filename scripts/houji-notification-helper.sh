#!/system/bin/sh
# Xiaomi 14 (houji - SM8650/pineapple) Stability, Notification & Performance Helper
# Place in /data/adb/service.d/houji-notification-helper.sh or run via KSU boot-completed.sh

export PATH="/data/adb/ksu/bin:/data/adb/ap/bin:/data/adb/magisk:/system/bin:/vendor/bin:$PATH"

# Wait for Android boot completion
while [ "$(getprop sys.boot_completed)" != "1" ]; do
  sleep 2
done

# Allow vendor post_boot scripts to finish initial zram0 setup first
sleep 3

# ==============================================================================
# 1. HyperOS Notification & Background Retention Safeguards
# ==============================================================================
# Disable Xiaomi Greezer explicit 10s screen-off GMS freeze limiter
dumpsys greezer IM GMS disable >/dev/null 2>&1 || true

# Ensure com.google.android.gms is in MILLET_NO_RESTRICT_APP to keep FCM TCP 5228 alive
CUR_LIST=$(settings --user 0 get system MILLET_NO_RESTRICT_APP 2>/dev/null)
case "$CUR_LIST" in
  *com.google.android.gms*) ;;
  null|"") settings --user 0 put system MILLET_NO_RESTRICT_APP "com.google.android.gms" ;;
  *) settings --user 0 put system MILLET_NO_RESTRICT_APP "${CUR_LIST},com.google.android.gms" ;;
esac

# Disable Android Phantom Process Killer to expand background app retention
device_config put activity_manager max_phantom_processes 2147483647 >/dev/null 2>&1 || true

# ==============================================================================
# 2. Qualcomm SM8650 (pineapple 2_3_2_1) CPU & Deep Sleep Tuning
# ==============================================================================
# Prevent minor RT tasks from pinning Cortex-X4 at max freq (Qualcomm SM8650 reference)
sysctl -w kernel.sched_util_clamp_min_rt_default=0 >/dev/null 2>&1 || true

# Verify deep sleep mode is s2idle on SM8650 and set freeze timeout to 2s
if [ -f /sys/power/mem_sleep ]; then
  echo s2idle > /sys/power/mem_sleep 2>/dev/null || true
fi
if [ -f /sys/power/pm_freeze_timeout ]; then
  echo 2000 > /sys/power/pm_freeze_timeout 2>/dev/null || true
fi

# Silence noisy printk console logspam
if [ -f /proc/sys/kernel/printk ]; then
  echo "4 4 1 7" > /proc/sys/kernel/printk 2>/dev/null || true
fi

# ==============================================================================
# 3. Virtual Memory & 1:1 ZRAM Sizing (8GB RAM -> 8GB ZRAM, 12GB -> 12GB, 16GB -> 16GB)
# ==============================================================================
# Unlock HyperOS 4GB ZRAM cap (/vendor/etc/fstab.qcom) and match ZRAM 1:1 with physical RAM
ZRAM_DEV="/dev/block/zram0"
[ ! -b "$ZRAM_DEV" ] && [ -b "/dev/zram0" ] && ZRAM_DEV="/dev/zram0"
if [ -b "$ZRAM_DEV" ] && [ -f /sys/block/zram0/disksize ] && [ -f /sys/block/zram0/reset ]; then
  MEM_TOTAL_KB=$(sed -n 's/^MemTotal:[[:space:]]*\([0-9]*\).*/\1/p' /proc/meminfo 2>/dev/null)
  if [ -n "$MEM_TOTAL_KB" ]; then
    if [ "$MEM_TOTAL_KB" -le 8388608 ]; then
      TARGET_ZRAM_BYTES="8589934592"   # 8 GB
    elif [ "$MEM_TOTAL_KB" -le 12582912 ]; then
      TARGET_ZRAM_BYTES="12884901888"  # 12 GB
    elif [ "$MEM_TOTAL_KB" -le 16777216 ]; then
      TARGET_ZRAM_BYTES="17179869184"  # 16 GB
    else
      TARGET_ZRAM_BYTES="25769803776"  # 24 GB
    fi

    CUR_ZRAM_BYTES=$(cat /sys/block/zram0/disksize 2>/dev/null)
    if [ "$CUR_ZRAM_BYTES" != "$TARGET_ZRAM_BYTES" ]; then
      CUR_ALG=$(sed -n 's/.*\[\([^]]*\)\].*/\1/p' /sys/block/zram0/comp_algorithm 2>/dev/null)
      swapoff "$ZRAM_DEV" >/dev/null 2>&1 || /system/bin/swapoff "$ZRAM_DEV" >/dev/null 2>&1 || true
      echo 1 > /sys/block/zram0/reset 2>/dev/null || true
      if [ -n "$CUR_ALG" ] && [ -f /sys/block/zram0/comp_algorithm ]; then
        echo "$CUR_ALG" > /sys/block/zram0/comp_algorithm 2>/dev/null || true
      fi
      echo "$TARGET_ZRAM_BYTES" > /sys/block/zram0/disksize 2>/dev/null || true
      mkswap "$ZRAM_DEV" >/dev/null 2>&1 || /system/bin/mkswap "$ZRAM_DEV" >/dev/null 2>&1 || true
      swapon "$ZRAM_DEV" -p 32758 >/dev/null 2>&1 || swapon "$ZRAM_DEV" >/dev/null 2>&1 || /system/bin/swapon "$ZRAM_DEV" >/dev/null 2>&1 || true
    fi
  fi
fi

# Disable swap readahead (ZRAM is in-memory; cluster reads cause unnecessary CPU uncompression)
sysctl -w vm.page-cluster=0 >/dev/null 2>&1 || true

# Disable proactive background compaction to prevent UI micro-stutters
sysctl -w vm.compaction_proactiveness=0 >/dev/null 2>&1 || true

# Retain dentry and inode cache longer in LPDDR5X for faster app relaunch
sysctl -w vm.vfs_cache_pressure=80 >/dev/null 2>&1 || true

# Smooth dirty page flushing to avoid I/O bottlenecks
sysctl -w vm.dirty_background_ratio=5 >/dev/null 2>&1 || true
sysctl -w vm.dirty_ratio=15 >/dev/null 2>&1 || true

# ==============================================================================
# 4. Storage I/O & F2FS Runtime Tuning (Zero C-code risk)
# ==============================================================================
# Disable I/O stats overhead on UFS 4.0 block devices and set 128KB readahead
for dev in /sys/block/sd*; do
  if [ -d "$dev/queue" ]; then
    echo 0 > "$dev/queue/iostats" 2>/dev/null || true
    echo 128 > "$dev/queue/read_ahead_kb" 2>/dev/null || true
  fi
done

# Tune F2FS parameters via sysfs (Rule 5 & 8: replaces risky C-code patches)
for f2fs_dir in /sys/fs/f2fs/*; do
  if [ -d "$f2fs_dir" ]; then
    [ -f "$f2fs_dir/min_fsync_blocks" ] && echo 20 > "$f2fs_dir/min_fsync_blocks" 2>/dev/null || true
    [ -f "$f2fs_dir/gc_urgent_sleep_time" ] && echo 50 > "$f2fs_dir/gc_urgent_sleep_time" 2>/dev/null || true
  fi
done

# ==============================================================================
# 5. Network & TCP BBRv3 Pacing Tuning
# ==============================================================================
sysctl -w net.core.default_qdisc=fq >/dev/null 2>&1 || true
sysctl -w net.ipv4.tcp_congestion_control=bbr >/dev/null 2>&1 || true
sysctl -w net.ipv4.tcp_fastopen=3 >/dev/null 2>&1 || true
sysctl -w net.ipv4.tcp_mtu_probing=1 >/dev/null 2>&1 || true
sysctl -w net.ipv4.tcp_ecn=1 >/dev/null 2>&1 || true

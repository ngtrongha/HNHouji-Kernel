# Houji: stock 6.1.157 display interfaces on ACK 6.1 LTS

Authorized by the user on 2026-11-05 for a new release build.

The supplied `stock/boot.img` contains
`6.1.157-android14-11-g78a0a76cd7b7-ab15497637`.
Its SHA-256 is `6f2eb517f47369572c07cb7261e395007fc9e3260abba1fba719f41a05683157`;
the extracted IKCONFIG SHA-256 is
`522f24a32e3dd6b5a76c418890a9aa54d6f37492ac8afa814b692692ef74c8eb`.
Google ACK resolves that source prefix to
[`78a0a76cd7b7029059bdf03eba04ebeaf9af4eed`](https://android.googlesource.com/kernel/common/+/78a0a76cd7b7029059bdf03eba04ebeaf9af4eed),
whose Makefile reports 6.1.157.

The LTS build keeps ACK `06aabb3c55412104814644e8d1b37df04940d99a`
(6.1.177), including its complete dma-buf subsystem, F2FS layout and memory/scheduler
code. Only these paths move from the old 6.1.138 stock pin to the new stock commit:

- `drivers/gpu/drm/`
- `include/drm/`
- `drivers/video/backlight/`
- `include/linux/backlight.h`
- `drivers/spmi/`

The new stock IKCONFIG retains HZ=250, 4 KiB pages, VA_BITS=39, PREEMPT,
THP/MADVISE, MGLRU, modular ZRAM/zsmalloc, lzo-rle, CFI and LTO_NONE.
Compared with the saved old stock Image, eight enabled settings are added:
ARCH_WANTS_DYNAMIC_TASK_STRUCT, GKI_DYNAMIC_TASK_STRUCT_SIZE,
GKI_TASK_STRUCT_VENDOR_SIZE_MAX=1024, IPC_NS, PAGE_BLOCK_ORDER=11,
POSIX_MQUEUE, POSIX_MQUEUE_SYSCTL and UDMABUF. This observation does not authorize
copying these settings into the retained ACK baseline.

The output remains a 6.1.177 kernel; its module still requires exact live build ID
and full IKCONFIG hash matches against CI-generated paired metadata. Component
resolution uses `commit_mode=latest`; version-mismatch bypass stays disabled.

Historical r8/r11 boot validation covers the old stock display pin. This new
combination requires device testing for boot, display/brightness, suspend and
`/data` behavior. The supplied boot alone does not identify the firmware version
or prove that the saved vendor_boot and other images belong to the same update.
The original stock images and previous extraction remain untouched.

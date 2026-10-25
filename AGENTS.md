# Xiaomi 14 (houji) GKI 6.1 Build & Runtime Error Prevention Rules

## Build mode preference (user instruction, 2026-10-24)
- Always default component resolution and Codex-triggered builds to `commit_mode=latest`; do not silently substitute `verified`. An explicit later user choice may override this preference.
- This preference tracks root/SUSFS/NoMount component branch tips. Preserve the houji ACK baseline and display/vendor compatibility shields below; do not infer permission to merge upstream kernel code or remove these shields from the commit-mode choice.

## Rule 0: Mandatory Error Ledger Update
Every time a CI build error or device runtime bug occurs, **you MUST add its symptom, root cause, and prevention rule to this `AGENTS.md` file before pushing the fix** so the same failure is never repeated.

---

## Rule 1: Never Set `CONFIG_ZRAM=y` or `CONFIG_ZSMALLOC=y` on GKI 6.1
- **Symptom**: Build fails at the end of `//common:kernel_aarch64` (after compiling `vmlinux`) with:
  ```
  ERROR: Unable to find drivers/block/zram/zram.ko in any of the following directories: ...
  Unable to find mm/zsmalloc.ko in any of the following directories: ...
  ```
- **Root Cause**: In Android 14 GKI 6.1 (`common/modules.bzl`), `drivers/block/zram/zram.ko` and `mm/zsmalloc.ko` are listed in `COMMON_GKI_MODULES_LIST` (`CONFIG_ZRAM=m`, `CONFIG_ZSMALLOC=m`). Setting `CONFIG_ZRAM=y` compiles both into `vmlinux` instead of producing `.ko` module files.
- **Prevention**: Leave `CONFIG_ZRAM=m` and `CONFIG_ZSMALLOC=m` untouched in `gki_defconfig`.

---

## Rule 2: Never Pin or Split `drivers/dma-buf/` or `dma-fence`/`dma-resv` Headers on `6.1.177`
- **Symptom**:
  1. Checking out full `drivers/dma-buf/` and `include/linux/dma-buf.h` from `deprecated/android14-6.1-2025-06` (`6.1.138`) breaks compilation of `fs/proc/base.c`, `mm/mmap.c`, `kernel/fork.c`, and `kernel/bpf/dmabuf_iter.c` (`incomplete definition of type 'struct task_dma_buf_info'`).
  2. Checking out only `dma-fence*.c`, `dma-resv.c`, `sync_file.c`, and `iosys-map.h` from `6.1.138` while keeping `dma-buf.c` and `heaps/` on `6.1.177` builds cleanly, **but causes a BLACK SCREEN immediately after the Xiaomi boot logo** when SurfaceFlinger / `msm_drm.ko` initializes.
- **Root Cause**: Splitting `drivers/dma-buf/` between `6.1.177` (`dma-buf.c`, `dma-heap.c`, `heaps/`) and `6.1.138` (`dma-fence.c`, `dma-resv.c`, `sync_file.c`) creates internal ABI/locking mismatches inside `dma-buf` when Gralloc/SurfaceFlinger attaches fences to display buffers.
- **Prevention**: Keep the entire `drivers/dma-buf/` subsystem and `include/linux/dma-*` / `sync_file.h` / `iosys-map.h` 100% untouched on `6.1.177` (`refs/heads/android14-6.1`). Pin **only** the 5 display/PMIC paths from `deprecated/android14-6.1-2025-06`:
  - `drivers/gpu/drm/`
  - `include/drm/`
  - `drivers/video/backlight/`
  - `include/linux/backlight.h`
  - `drivers/spmi/`

---

## Rule 3: Never Put Optional/Renamed Headers in a Multi-File `git checkout FETCH_HEAD -- ...`
- **Symptom**: `git checkout FETCH_HEAD -- ...` exits with code 1 (`error: pathspec 'include/linux/dma-buf-map.h' did not match any file(s) known to git`).
- **Root Cause**: `include/linux/dma-buf-map.h` was renamed to `include/linux/iosys-map.h`. A single missing pathspec aborts the entire `git checkout` command.
- **Prevention**: Only pass guaranteed paths in the main `git checkout FETCH_HEAD -- ...` block.

---

## Rule 4: `6.1-lts` Brightness Flickering & ACK Vendor Hook Alignment
- **Symptom**: Screen brightness flickers on Xiaomi 14 (`houji`) when running `6.1-lts`.
- **Root Cause**:
  1. `refs/heads/android14-6.1-lts` (and `refs/tags/android14-6.1.138_r00`) diverged from Google's ACK in April 2024 and lacks `ANDROID: GKI:` vendor hooks required by Xiaomi's `msm_drm.ko` and `mi_disp.ko`.
  2. Post-`6.1.138` DRM/backlight/SPMI changes alter VBlank and MIPI-DSI `0x51` brightness timing.
- **Prevention**: For `6.1-lts` builds, always sync `refs/heads/android14-6.1` (`6.1.177` LTS + full ACK vendor hooks) and surgically pin **only** `drivers/gpu/drm/`, `include/drm/`, `drivers/video/backlight/`, `include/linux/backlight.h`, and `drivers/spmi/` from `refs/heads/deprecated/android14-6.1-2025-06` (`6.1.138`).

---

## Rule 5: Never Patch Core Memory Reclaim, PSI, Scheduler, or Binder C Code
- **Symptom**: Device runs smoothly for 20–45 minutes, then hard-freezes (requiring forced reboot), or kills background apps/notifications.
- **Root Cause**:
  - Dampening `PSI_MEM_SOME`/`PSI_MEM_FULL` in `kernel/sched/psi.c` blinds Android's `lmkd` when 8GB RAM + ZRAM fills up, trapping all 8 CPU cores in an infinite `try_to_free_pages()` / `lru_lock` livelock.
  - Setting `watermark_boost_factor = 0` in `mm/page_alloc.c` or forcing MGLRU/scheduler/cgroup/binder C-code hacks causes spinlock deadlocks with HyperOS vendor modules (`mi_schedule.ko`, `metis.ko`, `binder_prio.ko`).
- **Prevention**: Keep `mm/`, `kernel/sched/`, `kernel/cgroup/`, `drivers/android/binder*`, `drivers/block/zram/`, and `fs/` 100% pure upstream Google ACK C code (except official KernelSU/SUSFS patches).

---

## Rule 6: Never Enable `CONFIG_TRANSPARENT_HUGEPAGE=y` or Override Stock `gki_defconfig` MM/ZRAM Options on `houji` 6.1
- **Symptom**: Kernel passes `kmi_check.py` (`8909 symbols checked, no CRC mismatch`), flashes via AnyKernel3, shows the Xiaomi boot logo, and then **immediately goes to a black screen**.
- **Root Cause**:
  - Stock Android 14 GKI 6.1 (`arch/arm64/configs/gki_defconfig`) has `# CONFIG_TRANSPARENT_HUGEPAGE is not set`. Enabling `CONFIG_TRANSPARENT_HUGEPAGE=y` (even with `MADVISE=y`) changes inline folio/page/PMD helper definitions (`PageCompound`, `folio_test_large`, `pmd_trans_huge`) that are not caught by `vmlinux.symvers` symbol CRCs, breaking `msm_kgsl.ko` / `msm_drm.ko` GPU/display buffer mappings when SurfaceFlinger starts.
  - AnyKernel3 only flashes `Image` (not `system_dlkm`/`vendor_dlkm`), so overriding `CONFIG_ZRAM_DEF_COMP_*` or `CONFIG_LRU_GEN_ENABLED=y` mismatches the stock userspace/module expectations.
- **Prevention**: Do NOT add `CONFIG_TRANSPARENT_HUGEPAGE=y`, `CONFIG_TRANSPARENT_HUGEPAGE_MADVISE=y`, `CONFIG_LRU_GEN_ENABLED=y`, or `CONFIG_ZRAM_DEF_COMP_ZSTD=y` in `apply-device-patches/action.yml`. Keep the `gki_defconfig` identical to the proven working `15a8e28d` configuration.

---

## Rule 7: Compiler Flags and SELinux Weak Symbol
- **Never** add `-fno-plt` to `KBUILD_CFLAGS` (causes `RELA 311` error in `kvm_nvhe`).
- **Always** append `-Wno-vla -Wno-error=vla` to `Makefile` when building pinned `drivers/gpu/drm/`.
- **Always** convert `extern struct selinux_state *fake_state;` in `security/selinux/selinuxfs.c` to `struct selinux_state *fake_state __attribute__((weak)) = NULL;` so ReSukiSU builds do not fail with `ld.lld: error: undefined symbol: fake_state`.

---

## Rule 8: Preserve F2FS Struct Layout When Tracking `android14-6.1` Upstream HEAD
- **Symptom**: Kernel passes `kmi_check.py` (0 CRC mismatches), compiles cleanly, flashes via AnyKernel3, shows Xiaomi boot logo, and then **immediately goes to a black screen**.
- **Root Cause**: On Oct 15, 2026 at 21:55 UTC, Google ACK upstream pushed 27 commits modifying `fs/f2fs/` (`a465432321`, `745bced530`, etc.) that inserted `resizable_tail_secno`, `first_seq_zone_segno`, and `pinned_area_max_secno` into `struct f2fs_mount_info` and `struct f2fs_sb_info` in `fs/f2fs/f2fs.h`. Because `fs/f2fs/` internal struct layouts are not tracked by GKI KMI symbol CRCs, `kmi_check.py` passed with 0 mismatches, but Xiaomi 14's prebuilt vendor modules hooking `trace_android_vh_f2fs_iostat_*` dereferenced shifted struct offsets when `init` mounted `/data` (`f2fs`), crashing/hanging the device into an immediate black screen right after the bootloader logo.
- **Prevention**: When tracking the latest `refs/heads/android14-6.1` upstream HEAD, apply an ABI layout alignment in `fs/f2fs/f2fs.h` that relocates `first_seq_zone_segno`, `pinned_area_max_secno`, and `resizable_tail_secno` to the end of `struct f2fs_sb_info` (after `iostat_io_lat`). This preserves 100% of the byte offsets of `mount_opt`, `iostat_lock`, and `iostat_io_lat` expected by Xiaomi's prebuilt vendor modules (`mi_ext_lru.ko`, `mi_memory.ko`).

---

## Rule 9: Handle Single-Artifact Extraction in GitHub Actions `download-artifact` Verification
- **Symptom**: The `release` job fails at step `Verify all AnyKernel3 artifacts downloaded` with:
  ```
  ERROR: failed to re-fetch 6.1.177-android14-lts-KernelSU-Next-AnyKernel3
  expected an array but got: object ({"artifacts":[{"archive_d ...})
  ```
- **Root Cause**: When only a single artifact matches `pattern` across an Actions run (e.g. single-target `6.1-lts` + `KernelSU-Next`), `actions/download-artifact@v7` extracts files directly into `release-assets/` instead of `release-assets/<name>/`. Verification fails to find directory `release-assets/${name}`, attempts an API re-fetch with a broken jq query `.[0].archive_download_url`, and crashes.
- **Prevention**: Before verification in `main.yml`, detect if a single artifact was extracted flat into `release-assets/` and wrap it into `release-assets/${single_name}/`. Use `gh run download` directly as the fallback if an artifact is missing.

---

## Rule 10: Always Pin `drivers/gpu/drm/` to 6.1.138 on Xiaomi 14 (`houji`) 6.1-LTS
- **Symptom**: Kernel passes `kmi_check.py` (0 CRC mismatches), compiles cleanly, flashes via AnyKernel3, shows Xiaomi boot logo, then goes to a persistent **BLACK SCREEN**.
- **Root Cause**: Xiaomi 14's prebuilt vendor display modules (`msm_drm.ko` and `mi_disp.ko`) were compiled by Xiaomi against kernel 6.1.138. Linux 6.1.177 upstream made breaking internal changes to atomic display plane check callbacks and CRTC timing helpers. Running pure 6.1.177 DRM causes `msm_drm.ko` to fail display panel probe/modeset, leaving the screen completely blank (black screen).
- **Prevention**: Always surgically pin the 5 Display DRM and Backlight paths (`drivers/gpu/drm/`, `include/drm/`, `drivers/video/backlight/`, `include/linux/backlight.h`, and `drivers/spmi/`) to `refs/heads/deprecated/android14-6.1-2025-06` (`6.1.138`) while keeping `drivers/dma-buf/` on 6.1.177 and base ACK on verified commit `06aabb3c55`.

---

## Rule 11: Verified Ground-Truth Stock Kernel Baseline (HyperOS OS3.0.307.0.WNCCNXM)
- **Reference Firmware**: Xiaomi 14 (`houji`), HyperOS `OS3.0.307.0.WNCCNXM` (patch level 2026-08-01, SM8650 `pineapple`).
- **Stock Kernel Release**: `Linux version 6.1.138-android14-11-g0c3d559bcd85-ab14529422`
- **Official Google ACK Source Tag**: `refs/tags/android14-6.1-2025-06_r15` (`0c3d559bcd85c64dae9c9c9687abbe5347fff3ad`)
- **Official Compiler Toolchain**: Clang `r487747c` (`clang version 17.0.2`, LLD 17.0.2)
- **IKCONFIG Verified Parameters**:
  - `CONFIG_ARM64_4K_PAGES=y`, `CONFIG_ARM64_VA_BITS=39`, `CONFIG_HZ=250`, `CONFIG_PREEMPT=y`
  - `CONFIG_TRANSPARENT_HUGEPAGE=y`, `CONFIG_TRANSPARENT_HUGEPAGE_MADVISE=y`
  - `CONFIG_LRU_GEN=y`, `CONFIG_LRU_GEN_ENABLED=y`
  - `CONFIG_ZRAM=m`, `CONFIG_ZRAM_DEF_COMP="lzo-rle"`
  - `CONFIG_CFI_CLANG=y`, `CONFIG_LTO_NONE=y`
- **Prevention**: When upgrading to 6.1.177+ / 6.1.190, any display/SPMI compatibility checkout must target the exact stock tag `android14-6.1-2025-06_r15` (`0c3d559bcd85`), and defconfig settings for memory/scheduler must never deviate from these verified stock values.

---

## Rule 12: Certified Working Baseline Release 8 (r8) and Upstream 6.1.190 Rawdiff Integration
- **Verified Milestone**: [Release 8 (r8)](https://github.com/ngtrongha/GKI_KernelSU_SUSFS/releases/tag/r8), commit `a41a0b693`, base ACK commit `06aabb3c5541` + stock 6.1.138 DRM pin `0c3d559bcd85`. Verified by user device testing on 2026-10-16 to **boot cleanly on Xiaomi 14 (`houji`)** with zero black screen, zero brightness flickering, and working F2FS `/data` mount.
- **6.1.190 Integration Method**: In a shallow CI runner checkout, `git merge v6.1.190` fails with `fatal: refusing to merge unrelated histories`. To cleanly apply all bugfixes up to Linux 6.1.190, download and apply the official rawdiff between `v6.1.177` and `v6.1.190` from `git.kernel.org`:
  `https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/rawdiff/?id=v6.1.190&id2=v6.1.177`
  Immediately following rawdiff application:
  1. Re-pin the 5 Display DRM/Backlight/SPMI paths to stock tag `0c3d559bcd85` (`android14-6.1-2025-06_r15`).
  2. Restore `fs/f2fs/` to certified baseline `06aabb3c55` to keep the working struct layout.
  3. Clean up any reject files (`*.rej`) so reject scanners do not flag them.

---

## Rule 13: Always Pass `-F 3` to `patch -p1` in BBRv3 and Subsystem Backports on 6.1.190+
- **Symptom**: CI build fails at step `Setup Networking` / `Apply BBRv3 Patches` with:
  `common/net/ipv4/sysctl_net_ipv4.c.rej` generated (hunk #1 failed at line 44).
- **Root Cause**: Linux 6.1.190 upstream added `static int tcp_min_rcvbuf = 4096;` right above `sysctl_tcp_low_latency` in `net/ipv4/sysctl_net_ipv4.c`. Without fuzz tolerance (`-F 3`), GNU patch strictly rejects hunk #1 due to context line alteration even though the patch targets unrelated lines (`tcp_child_ehash_entries_max`).
- **Prevention**: Always pass `-F 3` (e.g. `patch -p1 -F 3 < ...`) in `.github/actions/bbrv3/action.yml` and `.github/actions/droidspaces/action.yml` so GNU patch cleanly tolerates up to 3 lines of upstream context drift.

---

## Rule 14: Never Apply Upstream Linux Stable Rawdiff Directly to Google ACK
- **Symptom**: Applying vanilla Linux stable rawdiff (e.g. `v6.1.177..v6.1.190`) from kernel.org onto Google Android Common Kernel (ACK) causes cascading compiler errors across multiple subsystems:
  1. `kernel/sched/psi.c: error: call to undeclared function 'timer_shutdown_sync'` and `no member named 'rtpoll_timer'`
  2. `include/linux/cma.h: error: redefinition of 'cma_allocator_trace_init'`
  3. `include/linux/writeback.h: note: previous declaration is here`
  4. Hunk rejections in `net/ipv4/sysctl_net_ipv4.c`
- **Root Cause**: Google ACK maintains Android-specific KMI vendor hooks (`android_vh_*`), custom tracepoints, and specific scheduler/memory interfaces that diverge from vanilla kernel.org Linux. Vanilla upstream point-releases (like 6.1.178–6.1.190) do not contain Android KMI adaptations until Google's ACK team merges and validates them on `refs/heads/android14-6.1`. Furthermore, 6.1.178..190 point releases contain no new user features or Qualcomm Snapdragon 8 Gen 3 (SM8650) fixes.
- **Prevention**: Never force rawdiff from kernel.org onto Google ACK. Always track official Google ACK tags/branches (`android14-6.1`), and build upon the certified verified baseline commit `06aabb3c55` + stock DRM pin `0c3d559bcd85` (Release 8).

---

## Rule 15: Performance Patches Safety & KMI Preservation on 6.1
- **Symptom**: Applying experimental performance patches directly without version gating causes KMI/ABI struct layout shift or F2FS data mount crash on Xiaomi 14 (`houji`).
- **Root Cause**:
  1. `file_struct_8bytes_align.patch` alters `__attribute__((aligned(4)))` to `aligned(8)` in `struct file` (`include/linux/fs.h`), changing the byte size and offsets of `struct file` passed between `vmlinux` and proprietary vendor modules.
  2. `reduce_gc_thread_sleep_time.patch`, `f2fs_reduce_congestion.patch`, and `f2fs_enlarge_min_fsync_blocks.patch` patch `fs/f2fs/` headers and C files directly, violating Rule 5 & Rule 8.
- **Prevention**: In `.github/actions/performance/action.yml`, always skip `file_struct_8bytes_align.patch` and all `fs/f2fs/` patches on `6.1`. Handle `min_fsync_blocks` and `gc_urgent_sleep_time` safely at runtime via sysfs in `scripts/houji-notification-helper.sh`. Only apply verified zero-KMI-impact ASM/power/memory patches (`clear_page_16bytes_align`, `optimise_memcmp`, `mem_opt_prefetch`, `avoid_extra_s2idle_wake_attempts`, `minimise_wakeup_time`, `reduce_freeze_timeout`, `int_sqrt`, `reduce_pci_pme_wakeups`, `increase_sk_mem_packets`, `silence_irq_cpu_logspam`, `silence_system_logspam`).

---

## Rule 16: Mandatory Upstream Review Before Merging
- **Rule**: NEVER automatically `git merge upstream/main` or blind cherry-pick upstream commits (`WildKernels/GKI_KernelSU_SUSFS`, Google ACK, etc.) into `main`.
- **Mandatory Procedure**:
  1. Fetch upstream changes via `git fetch upstream main --no-tags`.
  2. Inspect commits (`git log main..upstream/main`) and changed files (`git diff --stat main...upstream/main`).
  3. Categorize changes (CI/workflow, Telegram, root managers/pins, kernel source, detection evasion) and evaluate potential risks or conflicts with Xiaomi 14 (`houji`) hardware shields (Rules 1–15).
  4. Present a detailed breakdown report to the user and wait for explicit confirmation/decision before taking any merge action.

---

## Rule 17: Never Change `CONFIG_HZ=250` or Write `/proc/sys/walt/sched_ravg_window_nr_ticks` on GKI 6.1
- **Symptom**:
  1. Changing `CONFIG_HZ=250` to `300` or `1000` in `gki_defconfig` passes `kmi_check.py`, but causes premature display/GPU fence timeouts and rejected WALT scheduler sysctl writes on Xiaomi 14 (`houji`).
  2. Writing `echo 2 > /proc/sys/walt/sched_ravg_window_nr_ticks` at runtime in `scripts/houji-notification-helper.sh` triggers `stop_machine()` inside `walt.ko`, conflicting with Xiaomi's proprietary scheduler modules (`mi_schedule.ko` and `metis.ko`) and causing a **black screen + bootloop**.
- **Root Cause**:
  1. `msecs_to_jiffies()` in `include/linux/jiffies.h` is `static __always_inline` with `__builtin_constant_p(m)`, baking constant `250 Hz` (`1 jiffy = 4 ms`) jiffy counts into Xiaomi's prebuilt `.ko` modules (`msm_drm.ko`, `msm_kgsl.ko`, `ufshcd-qcom.ko`, `qca_cld3_peach_v2.ko`).
  2. HyperOS vendor scheduler modules (`mi_schedule.ko`, `metis.ko`) calibrate per-task load tracking windows strictly against the stock boot `sched_ravg_window_nr_ticks` value; mutating it at runtime deadlocks WALT window rollover.
- **Prevention**: Always keep `CONFIG_HZ_250=y` (`CONFIG_HZ=250`) untouched in `gki_defconfig` and **never** write to `/proc/sys/walt/sched_ravg_window_nr_ticks` in runtime scripts.

---

## Rule 18: Invalidate Bazel Disk Cache When Repository Name (`github.workspace`) Changes
- **Symptom**: After renaming the GitHub repository (e.g. from `GKI_KernelSU_SUSFS` to `HNHouji-Kernel`), `//common:kernel_aarch64` fails at the end of compilation with:
  ```
  .../bazel-out/k8-fastbuild/bin/build/kernel/hermetic-tools/tar: 3: /home/runner/work/GKI_KernelSU_SUSFS/GKI_KernelSU_SUSFS/kernel/prebuilts/build-tools/linux-x86/bin/toybox: not found
  make[3]: *** [kernel/kheaders_data.tar.xz] Error 127
  ```
- **Root Cause**: Kleaf's `@//build/kernel:hermetic-tools/*` wrapper scripts bake the absolute runner workspace path (`/home/runner/work/<repo>/<repo>/...`) into cached artifacts inside `/home/runner/.cache/bazel`. Restoring a `bazel-cache-*` release archive generated under the old repository name causes a Bazel disk-cache hit on `hermetic-tools/tar` pointing to the non-existent old workspace path.
- **Prevention**: Store `${{ github.workspace }}` in `/home/runner/.cache/bazel/.workspace_path` and automatically clear `/home/runner/.cache/bazel/*` in `.github/actions/build-kernel/action.yml` whenever `.workspace_path` is missing or differs from the current `${{ github.workspace }}`.

---

## Rule 19: Certified Working Release 11 (`r11`, Commit `91e410e23`) Baseline & `r12` Rollback
- **Symptom**: `Release 12 (r12)` (`806380a89`, built after merging upstream `502fd3b03` and adding WALT window tuning `44a055246`) caused a **black screen + bootloop** on Xiaomi 14 (`houji`), whereas `Release 11 (r11)` (`4576315c4` / `91e410e23`) booted and ran cleanly.
- **Root Cause**:
  1. Commit `44a055246` added `echo 2 > /proc/sys/walt/sched_ravg_window_nr_ticks` to `scripts/houji-notification-helper.sh`, triggering WALT `stop_machine()` conflict with HyperOS `mi_schedule.ko` / `metis.ko`.
  2. Commit `502fd3b03` merged upstream `.github/actions/stock-config` (replacing `kernel/config_data` `/proc/config.gz` with a generic AOSP prebuilt config lacking Xiaomi's expected config markers) and changed `susfs-setup` / `root-setup` pins (`pershoot/susfs4ksu` `6387756d6` & `161c058fe`).
- **Prevention**: Maintain the exact `Release 11 (r11)` (`91e410e23`) codebase and pin state (with only Rule 18's `.workspace_path` Bazel cache guard added). Never enable `.github/actions/stock-config` `/proc/config.gz` spoofing or mutate WALT window ticks on Xiaomi 14 (`houji`).

---

## Rule 20: Export User Reports to Shared Downloads, Not Root-Only State
- **Symptom**: HNHouji Experience WebUI reports a successful export to `/data/adb/houji-experience/report.json`, but the user cannot retrieve the file with a normal file manager.
- **Root Cause**: The export reused the private state writer and its root-only directory/0600 permissions, confusing internal storage with a user-facing download.
- **Prevention**: Export only the sanitized report to `/storage/emulated/0/Download/HNHouji/` using a unique filename and shared-storage-compatible creation permissions. Keep credentials, config and journal in private `/data/adb` storage. Verify the export completes before showing success; if shared storage is unavailable, report an error rather than silently falling back to private storage. Test destination, privacy, repeat exports and unavailable storage separately.

---

## Rule 21: Match namespace.c Include Preparation to the Selected SUSFS Patch
- **Symptom**: Run `40103361955` fails at SUSFS application with `Hunk #1 FAILED at 32` and `fs/namespace.c.rej` on ACK 6.1.177.
- **Root Cause**: The sublevel >=157 preparation unconditionally removes `#include <trace/hooks/blk.h>`, but latest SUSFS `27e0a0e42745` now requires that stock include in its namespace.c patch context. The other nine namespace hunks apply.
- **Prevention**: Inspect only the namespace.c section of the actual selected SUSFS patch: preserve/restore the stock include when the patch expects it, remove it temporarily only for older patch context, and restore it idempotently afterward. Fail on ambiguous/missing context; do not ignore rejects or remove vendor hooks from the compiled kernel. Verify against the certified ACK source and current SUSFS patch before pushing.

---

## Rule 22: Search mmap Kernel Images with find(), Not Sequence Membership
- **Symptom**: Run `40104094814` completes kernel compilation but fails when generating module metadata with `ValueError: vmlinux build ID is not present in this Image`.
- **Root Cause**: The metadata CLI maps Image with Python `mmap`. Multi-byte `needle in mmap` checks individual sequence elements and returns false even when the complete build ID is present; earlier tests used plain bytes and missed this production-only path.
- **Prevention**: Use `image.find(build_id_bytes) >= 0` for explicit substring search. Test the actual CLI with on-disk Image/vmlinux files so it exercises mmap, and retain a mismatched-image test that rejects invalid pairs without producing metadata. Never bypass the build ID check to make CI pass.

---

## Rule 23: Specify the Fork Repository When Downloading Paired Artifacts
- **Symptom**: Local end-to-end verification of successful run `40105396610` fails with HTTP 404 because `gh run download` requests the run from `WildKernels/GKI_KernelSU_SUSFS` instead of `ngtrongha/HNHouji-Kernel`.
- **Root Cause**: The collector lists artifacts using `GITHUB_REPOSITORY`, but its download command omits `--repo`, allowing gh's configured upstream/default repository to select a different repository.
- **Prevention**: Pass `--repo GITHUB_REPOSITORY` explicitly on every collector download, keeping repository, run ID and exact artifact name together. Test with a conflicting GH_REPO/default and verify real kernel/module artifacts from one completed run before release.

---

## Rule 24: Verify Paired Modules from Embedded Metadata and Account for Configured Uname Spoofing
- **Symptom**: Device report `HNHouji-report-20261024-154538` shows `manifest-missing`, all knobs waiting, and `paired:false`, although its full IKCONFIG SHA-256 and GNU build ID exactly match the successful CI kernel. Its reported uname differs from the compiled Image release.
- **Root Cause**: The module requires an external `/data/adb/houji-kernel/manifest.json` before inspecting its packaged `kernel.json`, so an absent recovery sidecar blocks even the correct paired module and misreports its paired status. Exact uname comparison also ignores that this verified kernel enables `CONFIG_KSU_SUSFS_SPOOF_UNAME=y`. The report does not establish why the recovery sidecar is absent.
- **Prevention**: Prefer the paired module's CI-generated embedded metadata; require the live GNU build ID, full IKCONFIG hash and baseline config policy to match it. Never manufacture metadata from runtime. Allow a different reported uname only when these identity checks pass and the matched config enables SPOOF_UNAME; surface both releases. Generic modules still require an installed sidecar, and invalid packaged metadata must never fall back to generic mode. Test missing/stale sidecars, wrong identity, invalid packages and spoofed/non-spoofed uname separately.

---

## Rule 25: Validate AI Completion Before Displaying Advice
- **Symptom**: The device displays an English analysis outline cut mid-sentence with 512 completion tokens, including an incorrect interpretation of PSI as standby power. The old UI does not expose finish_reason, so the specific request's truncation cause is unconfirmed.
- **Root Cause**: The parser accepts partial text without checking completion status, and the advisor prompt lacks precise metric definitions and a concise Vietnamese final-answer requirement. Reasoning models may share their token budget between reasoning and the visible answer.
- **Prevention**: Reject length/incomplete/tool/filter/error completions and fall back to local advice without automatic retries. Surface actual model, finish status and token usage; ignore separate reasoning fields and use OpenRouter's reasoning.exclude only to hide returned reasoning, never claim it eliminates reasoning cost. Define PSI, load average and battery temperature explicitly. Default new configurations to 1024 output tokens while preserving explicit saved limits. Daily analysis remains opt-in, reserves one attempt per local day before networking, sends a constructed aggregate without package names, and can never execute settings or commands. Test partial responses, privacy, scheduling, reset invalidation and manual review.

---

## Rule 26: Keep Manual AI Work Off Long WebUI Bridge Calls
- **Symptom**: The user reports that calling AI feels frozen, has no loading indicator, produces rigid hard-to-read output, and cannot exceed 2048 tokens.
- **Root Cause**: Manual API calls occupy a bridge exec until HTTP completes; the UI only disables one button and does not show progress/cancellation. Both HTML and Go enforce 2048 tokens, and the prompt additionally forces 180 words/four bullets. The assistant previously received a single sample for manual questions. Exact Android UI-thread stalls still require device profiling.
- **Prevention**: Start a short RPC job with a detached native worker, poll status, display elapsed waiting time, support cancellation, and reconcile interrupted/rebooted jobs without retries. Execute only the module's fixed binary with a random job ID, never AI content or credentials in argv. Render Markdown using text DOM nodes, not provider HTML. Supply measured daily comparisons, goal and bounded session follow-ups; keep package-name legends local. Zero output tokens means omit the token parameter and let the provider decide; positive explicit budgets have no module token ceiling. This supersedes Rule 25's old 1024-token default: migrate legacy 512/1024/2048 budgets once to provider defaults, default timeout 180 seconds (configurable 5..900), and retain future explicit choices. Bound HTTP/state bytes separately for memory use; allow larger AI result files without enlarging configuration limits. Verify loading/navigation/cancel, completion handling, large answers and privacy with mocks; verify native worker behavior on the device before claiming all freezes resolved.

---

## Rule 27: AI Settings Application Must Use a Closed, Validated Contract
- **Feature authorization**: The user requested an apply button and an option to apply after scheduled AI runs. This supersedes earlier advice-only behavior only for the typed settings below; arbitrary AI text/commands must still never execute.
- **Contract**: Accept a completed provider response with a strict JSON envelope containing Markdown answer and at most 18 typed actions. Resolve app IDs using the exact private request snapshot. Only app profiles (automatic/daily/native-game/windows-game/linux) and the existing daily kernel preset on/off are writable. Reject unknown fields/kinds, unsupported game modes, missing applications, stale/rebooted plans and changed baseline configuration. No source, pins, thermal/WALT/HZ, arbitrary paths, knob values, credentials or permission toggles may come from model output.
- **Application**: Manual apply uses a stored random plan ID and displays before/after/reasons. Scheduled auto-apply defaults off, requires current explicit opt-in plus at least two sampled days, 30 samples and 1800 observed seconds, preserves manually assigned app rules, and does not run for manually requested analyses. Recheck live kernel identity, owner user, module state, journal conflicts and supported modes; auto increases also require known PSI/thermal samples below the conservative guard thresholds. These counts/thresholds are experimental policy, not performance evidence.
- **Recovery/privacy**: Save affected config in one atomic transaction with a pending receipt; reconcile interrupted commits and undo without rerunning AI. Preserve the last successful undo receipt if a later attempt fails. Undo compares only affected fields and ownership and refuses to overwrite external changes. Kernel/game runtime restoration still uses the existing compare-and-restore journal. Redact raw and JSON-escaped credential echoes before parsing/display/persistence. Keep proposals/receipts private; never export them with the public report. Test malformed/mixed proposals, live guards, opt-out during request, idempotence, daily-vs-manual behavior, pressure, crash recovery and undo conflicts before release.

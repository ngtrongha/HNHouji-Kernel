# Houji build error ledger

## Verify the release string in the packaged Image

Run 44726388487 produced an ARM64 image with `Linux version 6.1.138-Wild`. All 8,466 compared symbol CRCs matched the ACK reference. The missing Android release and KMI generation were found during artifact inspection.

On stock ACK `0c3d559bcd85`, the Makefile prepends `KERNELVERSION` to the suffix printed by `scripts/setlocalversion`. The branding action detected KMI generation 11 but its legacy suffix branch emitted only the brand.

Preserve the Android release and KMI generation in suffix-only ACK scripts. The houji target must check the banner in the actual ARM64 Image for `6.1.138-android14-11-houji` before uploading AnyKernel3. Keep the ABI check as a separate gate.

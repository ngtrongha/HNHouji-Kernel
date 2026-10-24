#!/system/bin/sh
MODDIR=${0%/*}
# No work in a blocking boot stage. Bound boot-complete wait to five minutes.
count=0
while [ "$(/system/bin/getprop sys.boot_completed)" != 1 ]; do
  [ "$count" -ge 150 ] && exit 0
  count=$((count + 1))
  sleep 2
done
[ -f "$MODDIR/disable" ] && exit 0
[ -f "$MODDIR/remove" ] && exit 0
exec "$MODDIR/bin/houjictl" watch >/dev/null 2>&1

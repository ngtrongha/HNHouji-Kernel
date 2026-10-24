#!/system/bin/sh
MODDIR=${0%/*}
# On conflict the independent cleanup hook retains the journal for recovery.
"$MODDIR/bin/houjictl" uninstall >/dev/null 2>&1

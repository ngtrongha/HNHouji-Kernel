#!/system/bin/sh
# KSU extracts the module before customize.sh. Never change webroot contexts.
[ "$BOOTMODE" = true ] || abort "Install from the KernelSU Manager, not recovery."
[ -x "$MODPATH/bin/houjictl" ] || chmod 755 "$MODPATH/bin/houjictl"
"$MODPATH/bin/houjictl" install || abort "Device/state checks failed. See installation guide."
ui_print "HNHouji Experience preview installed."
if [ -f "$MODPATH/kernel.json" ]; then
  ui_print "Embedded kernel metadata found; WebUI verifies it against the running kernel."
else
  ui_print "Generic module: no embedded kernel metadata."
  ui_print "Requires matching installed kernel metadata or the paired module ZIP."
fi
ui_print "Reboot, then open WebUI. AI is optional and disabled by default."
ui_print "Default: Daily kernel preset, active after reboot only with matching build metadata."
ui_print "Existing helper archived if present; previous persistent settings are not guessed."

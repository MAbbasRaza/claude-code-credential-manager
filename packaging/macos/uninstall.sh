#!/bin/sh
# Removes everything the ccm installer package put on this Mac.
#
# Installed as /usr/local/bin/ccm-uninstall. A .pkg has no uninstaller of its
# own: macOS records what was installed but offers no way to undo it, so
# without this the only route is deleting files by hand and guessing which
# ones.
#
#   sudo ccm-uninstall
#
# Your saved accounts are deliberately left alone. See the note below.
set -eu

GUI_APP="/Applications/Claude Code Accounts.app"
TRAY_APP="/Applications/Claude Code Accounts Menu Bar.app"
PKG_IDS="com.mabbasraza.ccm.gui com.mabbasraza.ccm.tray com.mabbasraza.ccm.cli"

if [ "$(id -u)" -ne 0 ]; then
    echo "error: run this with sudo; the package installed into /Applications" >&2
    exit 1
fi

# Start-at-login is per-user and points into a bundle that is about to vanish,
# so a leftover entry would fail silently at every login. Removed for whoever
# is actually logged in, not for root, which is not the user who enabled it.
console_user="$(stat -f%Su /dev/console 2>/dev/null || true)"
if [ -n "$console_user" ] && [ "$console_user" != "root" ]; then
    plist="/Users/$console_user/Library/LaunchAgents/com.mabbasraza.ccm.ccm-tray.plist"
    if [ -f "$plist" ]; then
        echo "removing start-at-login for $console_user"
        su - "$console_user" -c "launchctl unload -w '$plist'" >/dev/null 2>&1 || true
        rm -f "$plist"
    fi
fi

# A running app holds its bundle open.
for app in "Claude Code Accounts" "Claude Code Accounts Menu Bar"; do
    pkill -f "/Applications/$app.app/" 2>/dev/null || true
done

for app in "$GUI_APP" "$TRAY_APP"; do
    if [ -e "$app" ]; then
        echo "removing $app"
        rm -rf "$app"
    fi
done

# Only if it still points into the bundle we just removed. A user who later
# installed the CLI another way, through Homebrew for instance, must keep it.
if [ -L /usr/local/bin/ccm ]; then
    target="$(readlink /usr/local/bin/ccm || true)"
    case "$target" in
        /Applications/Claude\ Code\ Accounts.app/*)
            echo "removing /usr/local/bin/ccm"
            rm -f /usr/local/bin/ccm
            ;;
        *)
            echo "leaving /usr/local/bin/ccm alone; it points at $target"
            ;;
    esac
fi

for id in $PKG_IDS; do
    if pkgutil --pkg-info "$id" >/dev/null 2>&1; then
        pkgutil --forget "$id" >/dev/null
    fi
done

echo
echo "Removed. Your saved accounts were left in place at"
echo "  ~/Library/Application Support/ccm"
echo
echo "They hold the credentials for every account you added, and deleting them"
echo "would force a browser sign-in for each one to recover. Remove that folder"
echo "yourself if you really want them gone."

# Deletes itself last. It lives in /usr/local/bin alongside the symlink and
# would otherwise be the one thing the uninstaller leaves behind.
rm -f /usr/local/bin/ccm-uninstall

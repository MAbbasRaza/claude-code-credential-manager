#!/bin/sh
# Builds the Debian package for ccm.
#
# The counterpart to scripts/build-installer.ps1 on Windows: the single place
# the package is assembled, called both by the release workflow and by hand, so
# what CI publishes and what a developer builds cannot drift.
#
# Like the Windows script it packages binaries that already exist rather than
# compiling them. ccm-tray and ccm-gui need cgo and the GTK, WebKit and
# AppIndicator development headers, so requiring a build here would mean the
# package could only ever be produced on a fully provisioned Linux machine.
#
# Deliberately POSIX sh with no bashisms, and it uses only dpkg-deb, which is
# present anywhere a .deb can be built. Confirmed necessary once already: the
# Ubuntu container this was tested in has /bin/sh as dash, not bash.
#
# Options, all environment variables so the script stays pipeable:
#
#   CCM_VERSION    release tag; default `git describe`
#   CCM_SRCDIR     directory holding ccm, ccm-tray, ccm-gui; default dist
#   CCM_OUTDIR     where the .deb is written; default dist
#   CCM_ARCH       Debian architecture; default amd64
set -eu

REPO_ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
cd "$REPO_ROOT"

VERSION="${CCM_VERSION:-}"
SRCDIR="${CCM_SRCDIR:-dist}"
OUTDIR="${CCM_OUTDIR:-dist}"
ARCH="${CCM_ARCH:-amd64}"

if [ -z "$VERSION" ]; then
    VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi

# relver is shared with the Windows installer. Debian's version field will not
# accept a leading v, and its ordering rules make a bare git hash meaningless,
# so the numeric form is used for the package and the full tag only for display.
eval "$(go run ./scripts/relver "$VERSION" | sed 's/^/rel_/')"
DEB_VERSION="$rel_short"
case "$rel_display" in
    *-*)
        # A prerelease must sort BEFORE the release it precedes. Debian orders
        # ~ lower than everything including the empty string, so 0.3.0~rc1 is
        # correctly older than 0.3.0, whereas 0.3.0-rc1 would be newer.
        DEB_VERSION="$rel_short~$(printf '%s' "$rel_display" | cut -d- -f2- | tr -c 'A-Za-z0-9.~' '.')"
        ;;
esac

echo "version   $rel_display  (deb ${DEB_VERSION}, arch ${ARCH})"

for prog in ccm ccm-tray ccm-gui; do
    if [ ! -f "$SRCDIR/$prog" ]; then
        echo "error: missing $SRCDIR/$prog" >&2
        echo "Point CCM_SRCDIR at a directory holding the three Linux binaries." >&2
        exit 1
    fi
done

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
ROOT="$STAGE/pkg"

install -d "$ROOT/DEBIAN"
install -d "$ROOT/usr/bin"
install -d "$ROOT/usr/share/applications"
install -d "$ROOT/usr/share/doc/ccm"

# Flat /usr/bin. internal/locate looks beside the running binary first, so the
# three find each other with no special handling on Linux; only macOS, where
# they ship as two application bundles, needs more.
for prog in ccm ccm-tray ccm-gui; do
    install -m 0755 "$SRCDIR/$prog" "$ROOT/usr/bin/$prog"
done

# Icons at the sizes internal/icon actually holds, rendered from the same
# embedded source the tray uses so the menu entry and the tray icon cannot
# drift. hicolor is the theme every desktop environment falls back to.
for size in 16 32 48 64 128; do
    dir="$ROOT/usr/share/icons/hicolor/${size}x${size}/apps"
    install -d "$dir"
    go run ./scripts/genpng "$size" "$dir/ccm.png"
done

cat > "$ROOT/usr/share/applications/ccm.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Claude Code Accounts
GenericName=Account Switcher
Comment=Switch between Claude Code accounts without signing in again
Exec=ccm-gui
Icon=ccm
Terminal=false
Categories=Development;
Keywords=claude;account;switch;credential;
StartupNotify=true
EOF

# The tray is a separate entry rather than a second Exec on the same one,
# because a user may want the window without a resident tray icon. NoDisplay is
# deliberately not set: it has to be launchable from the menu at least once, or
# enabling start-at-login is the only way to ever run it.
cat > "$ROOT/usr/share/applications/ccm-tray.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Claude Code Accounts (tray)
GenericName=Account Switcher
Comment=Tray icon for switching Claude Code accounts
Exec=ccm-tray
Icon=ccm
Terminal=false
Categories=Development;
StartupNotify=false
EOF

install -m 0644 LICENSE "$ROOT/usr/share/doc/ccm/copyright"

cat > "$ROOT/DEBIAN/control" <<EOF
Package: ccm
Version: ${DEB_VERSION}
Section: devel
Priority: optional
Architecture: ${ARCH}
Maintainer: MAbbasRaza <noreply@github.com>
Homepage: https://github.com/MAbbasRaza/claude-code-multi-account-manager
Depends: libc6, libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0 | libwebkit2gtk-4.0-37, libayatana-appindicator3-1
Description: Switch between Claude Code accounts without signing in again
 Claude Code stores one account's credentials at a time, so switching
 destroys the previous account's refresh token and returning requires a
 full browser sign-in.
 .
 ccm parks each account's credentials in a protected vault and swaps them
 back on demand, making a switch an offline, sub-second operation. Only
 the account-scoped keys are moved, so MCP server logins and project
 history are left untouched.
 .
 This package provides the ccm command, a desktop application and a tray
 applet.
EOF

# The alternatives are a package that fails to install on a headless server for
# want of a browser engine, or one that hides the dependency and produces a
# desktop app that dies on launch. Recommends is the wrong tool here: the tray
# and the window genuinely cannot run without these.
cat > "$ROOT/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e

if [ "$1" = "configure" ]; then
    # Refresh the caches so the menu entries and icons appear without a
    # re-login. Both are best effort: a server install has neither tool and
    # does not need either.
    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database -q /usr/share/applications || true
    fi
    if command -v gtk-update-icon-cache >/dev/null 2>&1; then
        gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
    fi

    # Start-at-login and a desktop shortcut are deliberately NOT created here.
    # dpkg runs as root while both are per-user, so there is no correct user to
    # create them for. `ccm autostart enable` and `ccm shortcut add` do it, and
    # so does the desktop app's Settings.
    #
    # The application menu entries above are different: they are system-wide by
    # nature, which is why the package can ship them directly.
    :
fi
exit 0
EOF
chmod 0755 "$ROOT/DEBIAN/postinst"

cat > "$ROOT/DEBIAN/postrm" <<'EOF'
#!/bin/sh
set -e

if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database -q /usr/share/applications || true
    fi
    if command -v gtk-update-icon-cache >/dev/null 2>&1; then
        gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
    fi
fi

# Per-user state is left alone, even on purge. That covers the vault and
# settings, which hold saved accounts whose loss would force a browser sign-in
# for every one, and also any desktop shortcut or autostart entry, which live in
# home directories dpkg has no business reaching into. `ccm shortcut remove`
# clears those, and they are inert once the binaries are gone in any case.
exit 0
EOF
chmod 0755 "$ROOT/DEBIAN/postrm"

# Root must own the payload, or dpkg installs files owned by the building
# user's uid. fakeroot when available; otherwise dpkg-deb's own --root-owner-group,
# which needs no privileges at all.
mkdir -p "$OUTDIR"
OUT="$OUTDIR/ccm-linux-${ARCH}.deb"
if command -v fakeroot >/dev/null 2>&1; then
    fakeroot dpkg-deb --build "$ROOT" "$OUT" >/dev/null
else
    dpkg-deb --root-owner-group --build "$ROOT" "$OUT" >/dev/null
fi

echo "built     $OUT"
if command -v sha256sum >/dev/null 2>&1; then
    echo "sha256    $(sha256sum "$OUT" | cut -d' ' -f1)"
fi

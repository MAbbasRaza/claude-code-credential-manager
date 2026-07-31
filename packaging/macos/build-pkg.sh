#!/bin/sh
# Builds the macOS installer package.
#
# The counterpart to scripts/build-installer.ps1 and packaging/linux/build-deb.sh:
# the single place the package is assembled, run both by the release workflow
# and by hand over SSH, so what CI publishes and what a developer builds cannot
# drift. Like both of those it packages binaries that already exist rather than
# compiling them.
#
# Options, all environment variables so the script stays pipeable:
#
#   CCM_VERSION            release tag; default `git describe`
#   CCM_SRCDIR             directory of ccm{,-tray,-gui}-darwin-{arm64,amd64}; default dist
#   CCM_OUTDIR             where the .pkg is written; default dist
#   MACOS_APP_IDENTITY     Developer ID Application identity; unset = ad-hoc
#   MACOS_INSTALLER_IDENTITY  Developer ID Installer identity; unset = unsigned
#
# CCM_OUTDIR receives exactly one file. The release workflow uploads whatever an
# artifact path contains and publishes all of it, so intermediates stay in a
# staging directory that is never uploaded.
set -eu

REPO_ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
cd "$REPO_ROOT"

VERSION="${CCM_VERSION:-}"
SRCDIR="${CCM_SRCDIR:-dist}"
OUTDIR="${CCM_OUTDIR:-dist}"
APP_IDENTITY="${MACOS_APP_IDENTITY:-}"
INSTALLER_IDENTITY="${MACOS_INSTALLER_IDENTITY:-}"

# These names are also in internal/locate's darwinBundles map, which is how the
# CLI finds the tray and the tray finds the desktop app once installed. Nothing
# at compile time checks the two agree, so the release workflow installs the
# package and asserts `ccm autostart status` names the tray inside its bundle.
GUI_APP="Claude Code Accounts.app"
TRAY_APP="Claude Code Accounts Menu Bar.app"

GUI_ID="com.mabbasraza.ccm.gui"
TRAY_ID="com.mabbasraza.ccm.tray"
CLI_ID="com.mabbasraza.ccm.cli"

for tool in pkgbuild productbuild lipo codesign plutil; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "error: $tool not found; this script only runs on macOS" >&2
        exit 1
    }
done

if [ -z "$VERSION" ]; then
    VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi
eval "$(go run ./scripts/relver "$VERSION" | sed 's/^/rel_/')"
echo "version   $rel_display  (CFBundleShortVersionString $rel_short)"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

# ---------------------------------------------------------------------------
# Universal binaries
# ---------------------------------------------------------------------------

# lipo strips the ad-hoc signature the Go linker applies, and Apple Silicon
# refuses to execute a binary carrying no signature at all. Every merged binary
# is therefore re-signed below. Skipping that produces a package that installs
# without complaint and then cannot launch on any M-series Mac, which is the
# single most expensive mistake available here.
sign_binary() {
    if [ -n "$APP_IDENTITY" ]; then
        codesign --force --timestamp --options runtime \
            --sign "$APP_IDENTITY" "$1"
    else
        codesign --force --sign - "$1"
    fi
}

merge() {
    prog="$1"
    out="$STAGE/bin/$prog"
    mkdir -p "$STAGE/bin"

    inputs=""
    for arch in arm64 amd64; do
        candidate="$SRCDIR/$prog-darwin-$arch"
        [ -f "$candidate" ] && inputs="$inputs $candidate"
    done
    # Accept the plain name too, so a developer who just ran `go build` can
    # package what they have.
    if [ -z "$inputs" ] && [ -f "$SRCDIR/$prog" ]; then
        inputs="$SRCDIR/$prog"
    fi
    if [ -z "$inputs" ]; then
        echo "error: no binary for $prog in $SRCDIR" >&2
        echo "Expected $prog-darwin-arm64 and/or $prog-darwin-amd64." >&2
        exit 1
    fi

    # shellcheck disable=SC2086
    set -- $inputs
    if [ "$#" -eq 1 ]; then
        # An Intel-only Mac can produce only an x86_64 slice. Allowed, so the
        # whole path stays testable on real hardware, but said out loud. The
        # release job asserts both slices separately, so a thin build cannot
        # reach a release.
        echo "warning: only one architecture for $prog; this package will not be universal" >&2
        cp "$1" "$out"
    else
        lipo -create -output "$out" "$@"
    fi
    chmod 0755 "$out"
    sign_binary "$out"
    echo "  $prog: $(lipo -archs "$out")"
}

echo "merging binaries"
merge ccm
merge ccm-tray
merge ccm-gui

# ---------------------------------------------------------------------------
# Icon
# ---------------------------------------------------------------------------

ICNS="$STAGE/icon.icns"
sh packaging/macos/make-icns.sh "$ICNS" >/dev/null
echo "icon      $(wc -c < "$ICNS" | tr -d ' ') bytes"

# ---------------------------------------------------------------------------
# Application bundles
#
# Two of them, because macOS derives an application's identity by walking up
# from the executable path: every binary inside one bundle shares that bundle's
# Info.plist, and a single plist cannot both set LSUIElement and not set it.
# The menu bar app must have it, the desktop app must not.
#
# The CLI rides inside the desktop app's bundle and is symlinked onto PATH by
# the postinstall script.
# ---------------------------------------------------------------------------

write_plist() {
    # $1 bundle Contents dir, $2 executable, $3 identifier, $4 display name,
    # $5 "yes" to set LSUIElement
    cat > "$1/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>              <string>$4</string>
  <key>CFBundleDisplayName</key>       <string>$4</string>
  <key>CFBundleExecutable</key>        <string>$2</string>
  <key>CFBundleIdentifier</key>        <string>$3</string>
  <key>CFBundleIconFile</key>          <string>icon</string>
  <key>CFBundlePackageType</key>       <string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$rel_short</string>
  <key>CFBundleVersion</key>           <string>$rel_short</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>LSMinimumSystemVersion</key>    <string>11.0</string>
  <key>NSHighResolutionCapable</key>   <true/>
PLIST
    if [ "$5" = "yes" ]; then
        # No Dock tile, no Cmd-Tab entry, no menu bar. What a status item app
        # is supposed to be.
        printf '  <key>LSUIElement</key>              <true/>\n' >> "$1/Info.plist"
    fi
    printf '</dict>\n</plist>\n' >> "$1/Info.plist"
    plutil -lint "$1/Info.plist" >/dev/null
}

build_bundle() {
    # $1 app name, $2 root under STAGE, $3 identifier, $4 display name,
    # $5 LSUIElement, then one or more executables. The first executable
    # becomes CFBundleExecutable; any others just ride along in the same
    # directory so internal/locate finds them beside it.
    app="$2/Applications/$1"
    ident="$3"; display="$4"; uielement="$5"
    shift 5

    mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
    cp "$ICNS" "$app/Contents/Resources/icon.icns"
    for prog in "$@"; do
        cp "$STAGE/bin/$prog" "$app/Contents/MacOS/$prog"
    done
    write_plist "$app/Contents" "$1" "$ident" "$display" "$uielement"

    # Signed after assembly, or the signature would not cover Info.plist and
    # the icon. --deep is deprecated; signing the bundle itself is enough here
    # because the nested executables were signed as they were merged.
    if [ -n "$APP_IDENTITY" ]; then
        codesign --force --timestamp --options runtime --sign "$APP_IDENTITY" "$app"
    else
        codesign --force --sign - "$app"
    fi
    codesign --verify --strict "$app"
}

echo "assembling bundles"
mkdir -p "$STAGE/root-gui" "$STAGE/root-tray"
build_bundle "$GUI_APP"  "$STAGE/root-gui"  "$GUI_ID"  "Claude Code Accounts"          no  ccm-gui ccm
build_bundle "$TRAY_APP" "$STAGE/root-tray" "$TRAY_ID" "Claude Code Accounts Menu Bar" yes ccm-tray

# ---------------------------------------------------------------------------
# CLI component: the uninstaller, plus a postinstall that puts ccm on PATH
# ---------------------------------------------------------------------------

mkdir -p "$STAGE/root-cli/usr/local/bin" "$STAGE/scripts-cli"
install -m 0755 packaging/macos/uninstall.sh "$STAGE/root-cli/usr/local/bin/ccm-uninstall"

cat > "$STAGE/scripts-cli/postinstall" <<'POSTINSTALL'
#!/bin/sh
set -e

# /usr/local/bin is on the default PATH but does not exist on a clean macOS.
mkdir -p /usr/local/bin

# A symlink rather than a copy, so an upgrade that replaces the bundle does not
# leave a stale CLI behind. internal/locate resolves symlinks before looking for
# siblings, which is what lets the CLI find the tray in the other bundle from
# here.
ln -sf "/Applications/Claude Code Accounts.app/Contents/MacOS/ccm" /usr/local/bin/ccm

# Start-at-login is deliberately not registered. This script runs as root and
# the LaunchAgent is per-user, so there is no correct user to register for.
# `ccm autostart enable` does it, and so does the desktop app's Settings.
exit 0
POSTINSTALL
chmod 0755 "$STAGE/scripts-cli/postinstall"

# ---------------------------------------------------------------------------
# Component packages
# ---------------------------------------------------------------------------

# BundleIsRelocatable=false. Left at its default of true, the installer hunts
# for an existing copy anywhere Spotlight knows about and installs over that
# instead of /Applications, which would silently break the symlink and the
# bundle lookup both.
component_plist() {
    cat > "$2" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<array>
  <dict>
    <key>BundleHasStrictIdentifier</key><true/>
    <key>BundleIsRelocatable</key>      <false/>
    <key>BundleIsVersionChecked</key>   <true/>
    <key>BundleOverwriteAction</key>    <string>upgrade</string>
    <key>RootRelativeBundlePath</key>   <string>Applications/$1</string>
  </dict>
</array>
</plist>
PLIST
    plutil -lint "$2" >/dev/null
}

echo "building components"
component_plist "$GUI_APP"  "$STAGE/component-gui.plist"
component_plist "$TRAY_APP" "$STAGE/component-tray.plist"

pkgbuild --root "$STAGE/root-gui"  --component-plist "$STAGE/component-gui.plist" \
    --identifier "$GUI_ID"  --version "$rel_short" --install-location / "$STAGE/gui.pkg" >/dev/null
pkgbuild --root "$STAGE/root-tray" --component-plist "$STAGE/component-tray.plist" \
    --identifier "$TRAY_ID" --version "$rel_short" --install-location / "$STAGE/tray.pkg" >/dev/null
pkgbuild --root "$STAGE/root-cli"  --scripts "$STAGE/scripts-cli" \
    --identifier "$CLI_ID"  --version "$rel_short" --install-location / "$STAGE/cli.pkg" >/dev/null

# ---------------------------------------------------------------------------
# The wizard
# ---------------------------------------------------------------------------

cat > "$STAGE/distribution.xml" <<DIST
<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="2">
    <title>Claude Code Accounts</title>
    <organization>com.mabbasraza</organization>
    <options customize="allow" require-scripts="false" hostArchitectures="arm64,x86_64"/>
    <volume-check>
        <allowed-os-versions><os-version min="11.0"/></allowed-os-versions>
    </volume-check>
    <welcome file="welcome.html" mime-type="text/html"/>
    <license file="LICENSE" mime-type="text/plain"/>
    <conclusion file="conclusion.html" mime-type="text/html"/>

    <choices-outline>
        <line choice="gui"/>
        <line choice="tray"/>
    </choices-outline>

    <choice id="gui" title="Claude Code Accounts"
            description="The desktop app and the ccm command line tool.">
        <pkg-ref id="$GUI_ID"/>
        <pkg-ref id="$CLI_ID"/>
    </choice>
    <choice id="tray" title="Menu bar app"
            description="A menu bar icon for switching accounts without opening anything.">
        <pkg-ref id="$TRAY_ID"/>
    </choice>

    <pkg-ref id="$GUI_ID"  version="$rel_short" onConclusion="none">gui.pkg</pkg-ref>
    <pkg-ref id="$TRAY_ID" version="$rel_short" onConclusion="none">tray.pkg</pkg-ref>
    <pkg-ref id="$CLI_ID"  version="$rel_short" onConclusion="none">cli.pkg</pkg-ref>
</installer-gui-script>
DIST

mkdir -p "$STAGE/resources"
cp packaging/macos/resources/welcome.html    "$STAGE/resources/welcome.html"
cp packaging/macos/resources/conclusion.html "$STAGE/resources/conclusion.html"
cp LICENSE "$STAGE/resources/LICENSE"

mkdir -p "$OUTDIR"
OUT="$OUTDIR/ccm-macos.pkg"

productbuild \
    --distribution "$STAGE/distribution.xml" \
    --resources "$STAGE/resources" \
    --package-path "$STAGE" \
    "$STAGE/unsigned.pkg" >/dev/null

if [ -n "$INSTALLER_IDENTITY" ]; then
    productsign --sign "$INSTALLER_IDENTITY" "$STAGE/unsigned.pkg" "$OUT"
else
    # No Developer ID configured. The package still installs, but Gatekeeper
    # will not open it from Finder; docs/SETUP.md covers the Terminal route.
    cp "$STAGE/unsigned.pkg" "$OUT"
fi

echo
echo "built     $OUT"
echo "size      $(wc -c < "$OUT" | tr -d ' ') bytes"
if command -v shasum >/dev/null 2>&1; then
    echo "sha256    $(shasum -a 256 "$OUT" | cut -d' ' -f1)"
fi

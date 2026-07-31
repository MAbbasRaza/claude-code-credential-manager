#!/bin/sh
# Builds assets/icon.icns from the master artwork.
#
#   sh packaging/macos/make-icns.sh [output path]
#
# Not taken from internal/icon, which the Windows .ico and the tray icon both
# come from. That package stops at 128px on purpose, and its own comment
# explains why: a 256px entry measured 63 KB, seventy percent of the whole ICO
# container, for an image nothing there asks for. It is compiled into every
# binary, so the size argument is real.
#
# A Dock and Finder icon is the case that argument does not cover. It wants
# 512@2x, so 1024px, which is eight times the largest embedded rendering. Since
# it is needed once at package time on a machine that already ships Apple's
# imaging tools, it is cut from the master here rather than bloating all three
# executables to serve one file.
set -eu

REPO_ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
SRC="$REPO_ROOT/assets/icon.png"
OUT="${1:-$REPO_ROOT/assets/icon.icns}"

for tool in sips iconutil; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "error: $tool not found; this script only runs on macOS" >&2
        exit 1
    }
done

[ -f "$SRC" ] || { echo "error: missing $SRC" >&2; exit 1; }

# Refuse to upscale. sips will happily enlarge a small source into a blurry
# 1024px icon, and the result looks like a mistake rather than reporting one.
width="$(sips -g pixelWidth "$SRC" | awk '/pixelWidth:/ {print $2}')"
if [ -z "$width" ] || [ "$width" -lt 1024 ]; then
    echo "error: $SRC is ${width:-unknown}px wide; an icns needs at least 1024" >&2
    exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
SET="$WORK/ccm.iconset"
mkdir -p "$SET"

# The names are fixed by iconutil; it rejects anything else.
emit() {
    sips -z "$2" "$2" "$SRC" --out "$SET/$1" >/dev/null 2>&1
}
emit icon_16x16.png        16
emit icon_16x16@2x.png     32
emit icon_32x32.png        32
emit icon_32x32@2x.png     64
emit icon_128x128.png     128
emit icon_128x128@2x.png  256
emit icon_256x256.png     256
emit icon_256x256@2x.png  512
emit icon_512x512.png     512
emit icon_512x512@2x.png 1024

mkdir -p "$(dirname "$OUT")"
iconutil -c icns "$SET" -o "$OUT"

echo "wrote $OUT ($(wc -c < "$OUT" | tr -d ' ') bytes)"

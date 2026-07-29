#!/usr/bin/env bash
# Regenerates the Windows resource objects that give the executables an icon.
#
# A Go binary has no icon unless a .syso resource object sits beside its
# package, so without this the taskbar button, Alt-Tab and Explorer all show the
# generic default. The tray icon is set at runtime and is a separate thing; it
# looked correct while the executables did not.
#
# The .syso files are committed, because requiring windres on every contributor's
# machine to get a normal build would be worse than checking in two small
# binaries. Run this after changing the artwork:
#
#   bash scripts/genrsrc.sh
#
# Needs windres from mingw-w64 (binutils). w64devkit ships it.
set -euo pipefail

cd "$(dirname "$0")/.."

if ! command -v windres >/dev/null 2>&1; then
    echo "error: windres not found. Install mingw-w64 binutils, or add w64devkit's bin to PATH." >&2
    exit 1
fi

echo "regenerating assets/icon.ico from internal/icon"
go run ./scripts/genico

for cmd in ccm ccm-tray ccm-gui; do
    rc="cmd/${cmd}/app.rc"
    out="cmd/${cmd}/rsrc_windows_amd64.syso"
    if [ ! -f "$rc" ]; then
        echo "error: $rc is missing" >&2
        exit 1
    fi
    # -I assets so the .rc can name icon.ico without each command carrying its
    # own copy of the artwork.
    windres -I assets -i "$rc" -O coff -o "$out"
    echo "  $out ($(wc -c < "$out") bytes)"
done

echo
echo "Note: these are windows/amd64 only. Go links a .syso by the GOOS_GOARCH"
echo "suffix, so windows/arm64 builds carry no icon until an arm64 object is"
echo "generated with a windres that targets aarch64 PE."

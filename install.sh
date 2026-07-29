#!/bin/sh
# Installer for ccm, the Claude Code credential manager.
#
#   curl -fsSL https://raw.githubusercontent.com/MAbbasRaza/claude-code-multi-account-manager/main/install.sh | sh
#
# Options, all via environment variables so the script stays pipeable:
#
#   CCM_VERSION      tag to install (default: latest)
#   CCM_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
#   CCM_TRAY         set to 1 to also install the tray app, where available
#   CCM_GUI          set to 1 to also install the desktop app, where available
#   CCM_NO_VERIFY    set to 1 to skip checksum verification (not recommended)
#
# POSIX sh on purpose: this has to run under dash, busybox ash and bash alike.
# Deliberately no sudo anywhere. The default target is inside your home
# directory, so an installer piped from the internet never needs root.

set -eu

REPO="MAbbasRaza/claude-code-multi-account-manager"
VERSION="${CCM_VERSION:-latest}"
INSTALL_DIR="${CCM_INSTALL_DIR:-$HOME/.local/bin}"
INSTALL_TRAY="${CCM_TRAY:-0}"
INSTALL_GUI="${CCM_GUI:-0}"
NO_VERIFY="${CCM_NO_VERIFY:-0}"

# Colour only when stderr is a terminal, so piped logs stay clean.
if [ -t 2 ] && [ -z "${NO_COLOR:-}" ]; then
    C_BOLD=$(printf '\033[1m'); C_RED=$(printf '\033[31m')
    C_GREEN=$(printf '\033[32m'); C_YELLOW=$(printf '\033[33m')
    C_OFF=$(printf '\033[0m')
else
    C_BOLD=''; C_RED=''; C_GREEN=''; C_YELLOW=''; C_OFF=''
fi

info()  { printf '%s\n' "$*" >&2; }
warn()  { printf '%s%s%s\n' "$C_YELLOW" "$*" "$C_OFF" >&2; }
ok()    { printf '%s%s%s\n' "$C_GREEN" "$*" "$C_OFF" >&2; }
fail()  { printf '%s%s%s\n' "$C_RED" "error: $*" "$C_OFF" >&2; exit 1; }

need() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 is required but not installed"
}

detect_platform() {
    os=$(uname -s)
    case "$os" in
        Linux)   OS=linux ;;
        Darwin)  OS=darwin ;;
        MINGW*|MSYS*|CYGWIN*)
            fail "this looks like Git Bash or Cygwin on Windows.
Use the PowerShell installer instead:

  irm https://raw.githubusercontent.com/$REPO/main/install.ps1 | iex" ;;
        *)       fail "unsupported operating system: $os" ;;
    esac

    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)   ARCH=amd64 ;;
        arm64|aarch64)  ARCH=arm64 ;;
        *)              fail "unsupported architecture: $arch" ;;
    esac

    # A native arm64 Mac running this under Rosetta reports x86_64. Installing
    # the Intel build would work but run translated, so correct it.
    if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ]; then
        if [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = "1" ]; then
            info "detected Rosetta; installing the native arm64 build"
            ARCH=arm64
        fi
    fi
}

download() {
    # $1 url, $2 destination. Fails loudly on a 404 rather than saving an
    # error page, which would otherwise be installed as the binary.
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$2" "$1"
    else
        fail "either curl or wget is required"
    fi
}

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | cut -d' ' -f1
    else
        return 1
    fi
}

check_version_tag() {
    # The tag is interpolated into a download URL, so reject anything that
    # could steer that URL elsewhere, such as a value containing a slash or a
    # traversal sequence.
    [ "$VERSION" = latest ] && return 0
    case "$VERSION" in
        v[0-9]*.[0-9]*.[0-9]*|[0-9]*.[0-9]*.[0-9]*) ;;
        *) fail "invalid CCM_VERSION '$VERSION'. Expected a tag such as v1.2.3, or 'latest'." ;;
    esac
    case "$VERSION" in
        */*|*..*|*' '*) fail "invalid CCM_VERSION '$VERSION': must not contain a path separator." ;;
    esac
}

release_url() {
    # $1 asset name
    if [ "$VERSION" = latest ]; then
        printf 'https://github.com/%s/releases/latest/download/%s' "$REPO" "$1"
    else
        printf 'https://github.com/%s/releases/download/%s/%s' "$REPO" "$VERSION" "$1"
    fi
}

install_asset() {
    # $1 asset name, $2 installed name
    asset="$1"
    target="$2"

    info "downloading $asset"
    download "$(release_url "$asset")" "$TMPDIR_CCM/$asset" \
        || fail "download failed for $asset.
If this is the tray app, it may not ship for your platform; see the README."

    if [ "$NO_VERIFY" != "1" ]; then
        # Fail closed. Every branch that cannot prove the download is genuine
        # aborts rather than warning and continuing: an attacker who can drop
        # or corrupt one request must not be able to silently downgrade this
        # installer to no verification at all.
        #
        # The pattern tolerates both GNU (two spaces) and BSD (space-star,
        # binary mode) forms, and anchors the name so ccm-linux-amd64 cannot
        # match a line for ccm-tray-linux-amd64.
        expected=$(awk -v want="$asset" '
            { name = $2; sub(/^\*/, "", name); if (name == want) { print $1; exit } }
        ' "$TMPDIR_CCM/SHA256SUMS" 2>/dev/null || true)

        if [ -z "$expected" ]; then
            fail "no checksum published for $asset.
The release may be incomplete. Re-run with CCM_NO_VERIFY=1 only if you accept the risk."
        fi

        actual=$(sha256_of "$TMPDIR_CCM/$asset") || fail "no sha256 tool found (need sha256sum or shasum).
Install one, or re-run with CCM_NO_VERIFY=1 only if you accept the risk."

        if [ "$actual" != "$expected" ]; then
            fail "checksum mismatch for $asset
  expected $expected
  actual   $actual
Do not use this file. Please report it."
        fi
    fi

    chmod +x "$TMPDIR_CCM/$asset"
    mkdir -p "$INSTALL_DIR"
    # mv can fail across filesystems; cp then rm is portable.
    cp "$TMPDIR_CCM/$asset" "$INSTALL_DIR/$target.tmp$$"
    chmod +x "$INSTALL_DIR/$target.tmp$$"
    mv -f "$INSTALL_DIR/$target.tmp$$" "$INSTALL_DIR/$target"
    ok "installed $INSTALL_DIR/$target"
}

on_path() {
    case ":${PATH}:" in
        *":$1:"*) return 0 ;;
        *) return 1 ;;
    esac
}

path_hint() {
    shell_name=$(basename "${SHELL:-sh}")
    case "$shell_name" in
        zsh)  rc="$HOME/.zshrc" ;;
        bash) if [ "$OS" = darwin ]; then rc="$HOME/.bash_profile"; else rc="$HOME/.bashrc"; fi ;;
        fish) rc="$HOME/.config/fish/config.fish" ;;
        *)    rc="$HOME/.profile" ;;
    esac

    warn ""
    warn "$INSTALL_DIR is not on your PATH."
    if [ "$shell_name" = fish ]; then
        warn "Add it with:"
        info "  fish_add_path $INSTALL_DIR"
    else
        warn "Add it with:"
        info "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> $rc"
        info "  exec \$SHELL"
    fi
}

main() {
    need uname
    check_version_tag
    detect_platform

    TMPDIR_CCM=$(mktemp -d 2>/dev/null || mktemp -d -t ccm)
    trap 'rm -rf "$TMPDIR_CCM"' EXIT INT TERM

    info "${C_BOLD}Installing ccm${C_OFF} ($OS/$ARCH, version $VERSION)"

    if [ "$NO_VERIFY" != "1" ]; then
        download "$(release_url SHA256SUMS)" "$TMPDIR_CCM/SHA256SUMS" 2>/dev/null \
            || fail "could not download SHA256SUMS from the $VERSION release.
Either that release does not exist, or the network blocked it.

Check https://github.com/$REPO/releases
To install without verification anyway: CCM_NO_VERIFY=1 (not recommended)"
    fi

    install_asset "ccm-$OS-$ARCH" ccm

    if [ "$INSTALL_TRAY" = "1" ]; then
        install_asset "ccm-tray-$OS-$ARCH" ccm-tray
    fi

    if [ "$INSTALL_GUI" = "1" ]; then
        install_asset "ccm-gui-$OS-$ARCH" ccm-gui
    fi

    installed_version=$("$INSTALL_DIR/ccm" --version 2>/dev/null || echo "unknown")
    ok ""
    ok "$installed_version"

    if ! on_path "$INSTALL_DIR"; then
        path_hint
    fi

    info ""
    info "${C_BOLD}Next steps${C_OFF}"
    info "  ccm init            pin your Claude Code config directory"
    info "  ccm add work        save the account you are signed into now"
    info ""
    info "Capture your current account BEFORE running /logout in Claude Code."
    info "Logging out destroys the refresh token there would be nothing left to save."
}

main "$@"

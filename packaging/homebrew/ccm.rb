# Homebrew formula for ccm.
#
# This file is the source of truth; the release workflow rewrites the version
# and checksums here and mirrors it into the tap repository. Install with:
#
#   brew install MAbbasRaza/tap/ccm
#
# The binaries are prebuilt rather than compiled from source so that installing
# does not require a Go toolchain.
class Ccm < Formula
  desc "Switch between Claude Code accounts without signing in again"
  homepage "https://github.com/MAbbasRaza/claude-code-credential-manager"
  version "0.2.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-darwin-arm64"
      sha256 "e3e4cfb81741b17196569d22cee463a1d9c042ad945c9952ed3cb6293533f2ec"
    end
    on_intel do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-darwin-amd64"
      sha256 "b38e2f546b1a7f146a821734b4d4d8aab6b69c383f00115a56cc3d85bb7f6007"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-linux-arm64"
      sha256 "c5b7aa1a6208dd3c30950ebabe8e6a60826834caea8c94101373eea8c84c4c83"
    end
    on_intel do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-linux-amd64"
      sha256 "b7e78edf77fcc5570ef45e3c742b4e136727c1efa55e151dbda843ab70867d0c"
    end
  end

  def install
    # The release asset is the bare binary, named for its platform.
    bin.install Dir["ccm-*"].first => "ccm"
  end

  def caveats
    <<~EOS
      Get started:

        ccm init            pin your Claude Code config directory
        ccm add work        save the account you are signed into now

      Capture your current account BEFORE running /logout in Claude Code.
      Logging out destroys the refresh token there would be nothing left to save.
    EOS
  end

  test do
    assert_match "ccm", shell_output("#{bin}/ccm --version")
    # doctor exits non-zero only on a hard failure; a machine with no Claude
    # Code install still produces a report.
    system "#{bin}/ccm", "--help"
  end
end

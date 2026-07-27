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
  version "0.1.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-darwin-arm64"
      sha256 "b37c403992d9f3e2c313870ec65a3b0b12c24c92fa776b5be1cb2c739a42938a"
    end
    on_intel do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-darwin-amd64"
      sha256 "7cba1b2039e06eaef11ba0c60fa60fcb04f3e34cdcbb4104d2c85a5ac9f3c4b7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-linux-arm64"
      sha256 "9af2f10b35a9a389665fa81846b2d6512348838029e2713c4d85a2e23f229d3c"
    end
    on_intel do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-linux-amd64"
      sha256 "513779f09f2d3ca2b62b3bd6582fbb6caaa0dc751f8a8e4b8b7389181c05a4b7"
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

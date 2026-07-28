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
  version "0.1.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-darwin-arm64"
      sha256 "70b6ac780d6fe537a2e5eaa39642589d66a6807fd4d541b17704f8f4bce0f339"
    end
    on_intel do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-darwin-amd64"
      sha256 "6cdb6e7dea2439b2d276b14625a62d930c97d19fd4e3870d64c926af379cdccf"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-linux-arm64"
      sha256 "1f08c4d029710a37d50cc2109bcd205e9c54fb04f27c20912c6ca7695bfe8091"
    end
    on_intel do
      url "https://github.com/MAbbasRaza/claude-code-credential-manager/releases/download/v#{version}/ccm-linux-amd64"
      sha256 "95504e2107c6618f42921b3ce5900c62f885222210c64e186cda103c1d5dd6d5"
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

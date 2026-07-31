# Optional convenience wrapper. The raw go commands in CONTRIBUTING.md are the
# source of truth; nothing here is required to build or contribute.
#
# Windows users without make: run the underlying commands directly.

GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)
BIN     := bin

.PHONY: help
help:
	@echo "build      build the CLI into $(BIN)/"
	@echo "tray       build the tray app (needs cgo on macOS and Linux)"
	@echo "gui        build the desktop app (needs cgo)"
	@echo "test       run the full Go test suite"
	@echo "check      gofmt, vet and test; what CI gates on"
	@echo "fmt        rewrite sources with gofmt"
	@echo "extension  install deps and compile the VS Code extension"
	@echo "installer  build the installer for the host platform"
	@echo "clean      remove build output"

.PHONY: build
build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ ./cmd/ccm

.PHONY: tray
tray:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ ./cmd/ccm-tray

.PHONY: gui
gui:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ ./cmd/ccm-gui

# Delegates rather than reimplementing. Those scripts are also what CI runs, so
# a package built here and one published by a release cannot differ.
.PHONY: installer
installer: build tray gui
	@case "$$(uname -s 2>/dev/null || echo Windows)" in \
		Darwin) CCM_SRCDIR=$(BIN) CCM_OUTDIR=dist sh packaging/macos/build-pkg.sh ;; \
		Linux)  CCM_SRCDIR=$(BIN) CCM_OUTDIR=dist sh packaging/linux/build-deb.sh ;; \
		*)      echo "On Windows run: pwsh -File scripts/build-installer.ps1 -Build"; exit 1 ;; \
	esac

.PHONY: test
test:
	$(GO) test ./... -count=1

.PHONY: fmt
fmt:
	gofmt -w .

# Mirrors the CI gates so a contributor can reproduce a red build locally.
.PHONY: check
check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; \
		echo; echo "run: make fmt"; exit 1; \
	fi
	$(GO) vet ./...
	$(GO) test ./... -count=1

.PHONY: extension
extension:
	cd extension && npm ci && npm run compile

.PHONY: clean
clean:
	rm -rf $(BIN) dist assets/icon.icns extension/out extension/*.vsix

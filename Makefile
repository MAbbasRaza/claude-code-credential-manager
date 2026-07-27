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
	@echo "test       run the full Go test suite"
	@echo "check      gofmt, vet and test; what CI gates on"
	@echo "fmt        rewrite sources with gofmt"
	@echo "extension  install deps and compile the VS Code extension"
	@echo "clean      remove build output"

.PHONY: build
build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ ./cmd/ccm

.PHONY: tray
tray:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ ./cmd/ccm-tray

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
	rm -rf $(BIN) extension/out extension/*.vsix

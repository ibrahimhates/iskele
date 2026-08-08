BINARY      := iskeled
CMD_PKG     := ./cmd/iskeled
BIN_DIR     := bin

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

VERSION_PKG := github.com/ibrahimhates/iskele/internal/version
LDFLAGS     := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).BuildDate=$(BUILD_DATE)

# cgo is disabled everywhere: iskeled ships as a single static binary and must
# cross-compile to arm64 and armv7 without a C toolchain.
GO          := CGO_ENABLED=0 go
GOFILES     := $(shell find . -name '*.go' -not -path './web/*')

.DEFAULT_GOAL := build

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the iskeled binary into bin/
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(CMD_PKG)
	@echo "built $(BIN_DIR)/$(BINARY) ($(VERSION), $(COMMIT))"

.PHONY: run
run: ## Run iskeled with a local data dir and debug logging
	$(GO) run -ldflags '$(LDFLAGS)' $(CMD_PKG) \
		--data-dir ./.data --secret-key-file $(CURDIR)/.data/secret.key \
		--log-level debug --log-format text \
		--allowed-paths $(CURDIR)

.PHONY: test
test: ## Run the Go test suite with the race detector
	$(GO) test -race ./...

.PHONY: test-cover
test-cover: ## Run tests and write coverage.out / coverage.html
	$(GO) test -race -coverpkg=./... -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@$(GO) tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint (install: https://golangci-lint.run)
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found; see https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run

.PHONY: fmt
fmt: ## Format Go sources
	gofmt -w $(GOFILES)

.PHONY: fmt-check
fmt-check: ## Fail if any Go source is not gofmt-clean
	@unformatted=$$(gofmt -l $(GOFILES)); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: vuln
vuln: ## Scan dependencies with govulncheck
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

.PHONY: clean
clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html

.PHONY: check
check: fmt-check vet test ## Everything CI runs, minus the external linters

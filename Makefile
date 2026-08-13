BINARY      := iskeled
CMD_PKG     := ./cmd/iskeled
BIN_DIR     := bin
WEB_DIR     := web
NPM         ?= npm

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

# The race detector is implemented in C, so the test targets — and only the
# test targets — need cgo. The shipped binary stays static.
GOTEST      := CGO_ENABLED=1 go

GOFILES     := $(shell find . -name '*.go' -not -path './web/*')

# npm packages occasionally ship Go sources of their own (flatted does), and
# `./...` walks into node_modules and compiles them. Every Go command below
# takes this list instead of the wildcard so a dependency of the frontend
# cannot end up in our build, our vet output or our coverage numbers.
PKGS         = $(shell $(GO) list ./... | grep -v '/node_modules/')

.DEFAULT_GOAL := build

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: web build-go ## Build the frontend and the binary that embeds it

.PHONY: build-go
build-go: ## Build the iskeled binary only, using whatever is in web/dist
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(CMD_PKG)
	@echo "built $(BIN_DIR)/$(BINARY) ($(VERSION), $(COMMIT))"

.PHONY: web
web: ## Build the frontend into web/dist, which go:embed then bundles
	cd $(WEB_DIR) && $(NPM) ci && $(NPM) run build

.PHONY: web-dev
web-dev: ## Run the Vite dev server against a local iskeled
	cd $(WEB_DIR) && $(NPM) run dev

.PHONY: web-lint
web-lint: ## Lint and type-check the frontend
	cd $(WEB_DIR) && $(NPM) run lint

.PHONY: web-test
web-test: ## Run the frontend test suite
	cd $(WEB_DIR) && $(NPM) run test

.PHONY: gen-api
gen-api: ## Regenerate the TypeScript wire types from docs/openapi.yaml
	cd $(WEB_DIR) && $(NPM) run gen:api

.PHONY: run
run: ## Run iskeled with a local data dir and debug logging
	$(GO) run -ldflags '$(LDFLAGS)' $(CMD_PKG) \
		--data-dir ./.data --secret-key-file $(CURDIR)/.data/secret.key \
		--log-level debug --log-format text \
		--allowed-paths $(CURDIR)

.PHONY: test
test: ## Run the Go test suite with the race detector
	$(GOTEST) test -race $(PKGS)

.PHONY: test-cover
test-cover: ## Run tests and write coverage.out / coverage.html
	$(GOTEST) test -race -coverpkg=$(shell echo $(PKGS) | tr ' ' ',') \
		-coverprofile=coverage.out -covermode=atomic $(PKGS)
	$(GO) tool cover -html=coverage.out -o coverage.html
	@$(GO) tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## Run go vet
	$(GO) vet $(PKGS)

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
vuln: ## Scan dependencies with govulncheck (reviewed exceptions: scripts/vulncheck)
	@# The tool runs govulncheck itself: a scan that dies before reaching the
	@# vulnerability database still prints output that looks clean, and only
	@# the exit status tells the two apart.
	$(GO) run ./scripts/vulncheck $(PKGS)

.PHONY: clean
clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html
	find $(WEB_DIR)/dist -mindepth 1 ! -name .gitkeep -delete 2>/dev/null || true

.PHONY: check
check: fmt-check vet test ## Everything CI runs, minus the external linters

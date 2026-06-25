# yaggo — Yet Another OpenAPI Go Generator
#
# Run `make help` to list available targets.

GO          ?= go
BINARY      := yaggo
BINARY_PATH := ./cmd/$(BINARY)
OUT_DIR     := bin

EXAMPLES_DIR := examples
EXAMPLE_SPEC := $(EXAMPLES_DIR)/petstore.yaml
EXAMPLE_OUT  := $(EXAMPLES_DIR)/petstore
EXAMPLE_PKG  := petstore

.DEFAULT_GOAL := help
.PHONY: help all build install clean test test-short test-race cover fmt vet tidy check example example-build

help: ## Show this help.
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*##/ {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: check build ## Run fmt, vet, test, then build.

build: ## Build the yaggo binary into ./bin/.
	@mkdir -p $(OUT_DIR)
	$(GO) build -o $(OUT_DIR)/$(BINARY) $(BINARY_PATH)

install: ## go install yaggo into $GOPATH/bin.
	$(GO) install $(BINARY_PATH)

clean: ## Remove ./bin and coverage artifacts.
	rm -rf $(OUT_DIR) coverage.out

test: ## Run all tests including the slow integration test.
	$(GO) test ./...

test-short: ## Run tests, skipping the integration test that compiles generated code.
	$(GO) test -short ./...

test-race: ## Run tests with the race detector.
	$(GO) test -race ./...

cover: ## Run tests with coverage and print the total.
	$(GO) test -cover -coverprofile=coverage.out ./...
	@$(GO) tool cover -func=coverage.out | tail -1

fmt: ## Run gofmt on all packages.
	$(GO) fmt ./...

vet: ## Run go vet on all packages.
	$(GO) vet ./...

tidy: ## Run go mod tidy in the root and examples modules.
	$(GO) mod tidy
	cd $(EXAMPLES_DIR) && $(GO) mod tidy

check: fmt vet test ## Run fmt, vet, and test.

example: build ## Regenerate the committed examples/petstore package.
	./$(OUT_DIR)/$(BINARY) -spec $(EXAMPLE_SPEC) -out $(EXAMPLE_OUT) -package $(EXAMPLE_PKG)

example-build: ## go build the examples module.
	cd $(EXAMPLES_DIR) && $(GO) build ./...

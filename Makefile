.DEFAULT_GOAL := help

MODULE := github.com/ygelfand/libcountertop
BASE := $(shell cat VERSION 2>/dev/null || echo 0.0.0)
BUILD_DIR := bin
TIMEOUT ?= 300s
PKG ?= ./...
GOLANGCI := golangci-lint

##@ Development

.PHONY: build
build: ## Build every package
	go build ./...

.PHONY: web
web:
	npm --prefix web run build

.PHONY: web-test
web-test:
	npm --prefix web test

.PHONY: cross
cross: ## Cross-compile every package for the device targets
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build ./...
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -tags noasm ./...

.PHONY: test
test: ## Run tests (make test PKG=./pkg/media/... TIMEOUT=60s)
	go test -timeout $(TIMEOUT) $(PKG)

.PHONY: test-race
test-race: ## Run tests with the race detector
	go test -race -timeout $(TIMEOUT) $(PKG)

.PHONY: cover
cover: ## Run tests and open a coverage report
	@mkdir -p $(BUILD_DIR)
	go test -timeout $(TIMEOUT) -coverprofile=$(BUILD_DIR)/coverage.out $(PKG)
	go tool cover -html=$(BUILD_DIR)/coverage.out

.PHONY: fmt
fmt: ## Format Go source
	$(GOLANGCI) fmt

.PHONY: fmt-check
fmt-check: ## Fail when any Go source is unformatted
	$(GOLANGCI) fmt --diff

.PHONY: vet
vet: ## Run go vet for the host and the device
	go vet ./...
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI) run

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	go mod tidy

.PHONY: check
check: fmt-check vet lint test cross ## Format check, vet, lint, test and cross-compile

##@ Native

API ?= 27
PREFIX ?= countertop
NATIVE_OUT ?= $(abspath $(BUILD_DIR))/native-api$(API)

.PHONY: native
native: ## Build the native helpers with the NDK (make native API=25 PREFIX=echolocal)
	$(MAKE) -f native/Makefile all API=$(API) PREFIX=$(PREFIX) OUT="$(NATIVE_OUT)"

.PHONY: native-all
native-all: ## Build the native helpers for every supported API
	$(MAKE) native API=25 PREFIX=echolocal
	$(MAKE) native API=27 PREFIX=lanovo

.PHONY: test-native
test-native: ## Run the native wire tests with the host compiler and UBSan
	$(MAKE) -f native/Makefile test OUT="$(abspath $(BUILD_DIR))/native-tests"

##@ Release

.PHONY: version
version: ## Show the version in VERSION
	@echo v$(BASE)

.PHONY: release
release: ## Tag and push the version in VERSION
	@$(MAKE) --no-print-directory tag TAG=v$(BASE)

.PHONY: tag
tag:
	@test -n "$(TAG)" || { echo "usage: make release, or make tag TAG=v0.1.1"; exit 1; }
	@echo "$(TAG)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || { echo "$(TAG) is not vMAJOR.MINOR.PATCH"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "the working tree is dirty"; exit 1; }
	@git rev-parse -q --verify "refs/tags/$(TAG)" >/dev/null && { echo "$(TAG) already exists"; exit 1; } || true
	@$(MAKE) --no-print-directory check test-native
	git tag -a "$(TAG)" -m "libcountertop $(TAG)"
	git push origin "$(TAG)"
	GOPROXY=https://proxy.golang.org go list -m $(MODULE)@$(TAG)
	@echo "pushed $(TAG); consumers pin it with: go get $(MODULE)@$(TAG)"

.PHONY: clean
clean: ## Remove build outputs
	rm -rf $(BUILD_DIR)

##@ Help

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

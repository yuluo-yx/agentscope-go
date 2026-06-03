##@ Go build

MODULE_NAME := github.com/yuluo-yx/agentscope-go
GO_BUILD_FLAGS := -trimpath -buildvcs=false
LDFLAGS := -s -w
UNIT_PACKAGES := $(shell $(GO) list ./... 2>/dev/null | grep -v '/test/e2e/' | grep -v '/test/integration/' | grep -v '/test/architecture/' | grep -v '/benchmarks$$')
INTEGRATION_PACKAGES := $(shell $(GO) list ./test/integration/... 2>/dev/null)
E2E_PACKAGES := $(shell $(GO) list ./test/e2e/... 2>/dev/null)
ARCHITECTURE_PACKAGES := $(shell $(GO) list ./test/architecture/... 2>/dev/null)
DOCKER_TEST_IMAGE ?= ubuntu:latest

.PHONY: download
download: ## Download dependencies
	@$(LOG_TARGET)
	$(GO) mod download

.PHONY: tidy
tidy: ## Run go mod tidy
	@$(LOG_TARGET)
	$(GO) mod tidy -v

.PHONY: fmt
fmt: ## Run go fmt
	@$(LOG_TARGET)
	$(GO) fmt ./...
	gofmt -s -w .

.PHONY: fmt-check
fmt-check: ## Check go fmt without modifying files
	@$(LOG_TARGET)
	@files="$$(find . -type f -name '*.go' -not -path './.git/*')"; \
	unformatted="$$(gofmt -s -l $$files)"; \
	if [ -n "$$unformatted" ]; then \
		echo "$$unformatted"; \
		echo "go files are not formatted; run make fmt" >&2; \
		exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet
	@$(LOG_TARGET)
	$(GO) vet ./...

.PHONY: test
test: ## Run project tests except Docker-gated E2E paths
test: test-unit test-integration test-e2e test-architecture

.PHONY: test-all
test-all: ## Run all local Go tests in one go test invocation
	@$(LOG_TARGET)
	$(GO) test ./... -v -race

.PHONY: test-unit
test-unit: ## Run non-E2E Go tests with race detection
	@$(LOG_TARGET)
	$(GO) test $(UNIT_PACKAGES) -v -race

.PHONY: test-integration
test-integration: ## Run integration tests
	@$(LOG_TARGET)
	$(GO) test $(INTEGRATION_PACKAGES) -v -race

.PHONY: test-e2e
test-e2e: ## Run E2E tests that do not require external Docker setup
	@$(LOG_TARGET)
	$(GO) test $(E2E_PACKAGES) -v -race

.PHONY: test-architecture
test-architecture: ## Run architecture and package-structure tests
	@$(LOG_TARGET)
	$(GO) test $(ARCHITECTURE_PACKAGES) -v -race

.PHONY: docker-test-image
docker-test-image: ## Ensure the Docker test image exists locally
	@$(LOG_TARGET)
	docker image inspect "$(DOCKER_TEST_IMAGE)" >/dev/null 2>&1 || docker pull "$(DOCKER_TEST_IMAGE)"

.PHONY: test-e2e-docker
test-e2e-docker: ## Run Docker-backed E2E and integration tests
test-e2e-docker: docker-test-image
	@$(LOG_TARGET)
	AGENTSCOPE_E2E_DOCKER=1 AGENTSCOPE_TEST_DOCKER=1 AGENTSCOPE_DOCKER_IMAGE="$(DOCKER_TEST_IMAGE)" $(GO) test ./test/e2e/... ./test/integration/workspace/docker/... -v -race

.PHONY: ci
ci: ## Run CI-aligned checks for formatting, linting, spelling, security, and tests
ci: ci-tools fmt-check lint-go codespell-check markdown-lint security-check test

.PHONY: lint-go
lint-go: ## Run golangci-lint
	@$(LOG_TARGET)
	@golangci-lint version | grep -Eq "version $(GOLANGCI_LINT_VERSION_NUMBER)([^0-9.]|$$)" || (echo "golangci-lint $(GOLANGCI_LINT_VERSION) is required; run make install-golangcilint"; exit 1)
	golangci-lint run ./... --config tools/linter/go/.golangci.yml

.PHONY: security-check
security-check: ## Run local security checks
security-check: gitleaks-check

.PHONY: govulncheck
govulncheck: ## Run govulncheck against all Go packages
	@$(LOG_TARGET)
	-go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

.PHONY: gitleaks-check
gitleaks-check: ## Scan the working tree for committed secrets
	@$(LOG_TARGET)
	gitleaks detect --source . --no-banner --redact

.PHONY: clean
clean: ## Clean build artifacts
	@$(LOG_TARGET)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out

.PHONY: coverage
coverage: ## Run tests with coverage
	@$(LOG_TARGET)
	$(GO) test ./... -race -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: coverage-unit
coverage-unit: ## Run non-E2E tests with coverage
	@$(LOG_TARGET)
	$(GO) test $(UNIT_PACKAGES) -race -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -func=coverage.out | tail -1

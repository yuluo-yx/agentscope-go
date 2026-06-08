##@ Go build

MODULE_NAME := github.com/yuluo-yx/agentscope-go
GO_BUILD_FLAGS := -trimpath -buildvcs=false
LDFLAGS := -s -w
UNIT_PACKAGES := $(shell $(GO) list ./... 2>/dev/null | grep -v '/benchmarks$$')
DOCKER_TEST_IMAGE ?= ubuntu:latest
AGENT_SANDBOX_VERSION ?= v0.4.6
AGENT_SANDBOX_KIND_CLUSTER ?= agentscope-agent-sandbox
AGENT_SANDBOX_RUNTIME_IMAGE ?= agentscope-agent-sandbox-runtime:$(AGENT_SANDBOX_VERSION)
AGENTSCOPE_AGENT_SANDBOX_TEMPLATE ?= python-sandbox-template
AGENTSCOPE_AGENT_SANDBOX_NAMESPACE ?= default

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
test: ## Run project unit tests and the local E2E profile
test: test-unit test-e2e

.PHONY: test-all
test-all: test ## Run all default local Go and E2E tests

.PHONY: test-unit
test-unit: ## Run non-E2E Go tests with race detection
	@$(LOG_TARGET)
	$(GO) test $(UNIT_PACKAGES) -v -race

.PHONY: test-integration
test-integration: ## Run integration coverage through selected local E2E cases
	@$(LOG_TARGET)
	$(MAKE) E2E_PROFILE=local E2E_REPORT_DIR=e2e/reports/integration E2E_TESTS=agent-observe-permission-context,context-compression,workspace-local-files,workspace-offload,workspace-resource-lifecycle e2e-test-specific

.PHONY: test-e2e
test-e2e: ## Run local E2E profile without live provider keys
	@$(LOG_TARGET)
	$(MAKE) e2e-test-no-key

.PHONY: test-architecture
test-architecture: ## Run architecture coverage through selected local E2E cases
	@$(LOG_TARGET)
	$(MAKE) E2E_PROFILE=local E2E_REPORT_DIR=e2e/reports/architecture E2E_TESTS=facade-package-contract,model-provider-metadata-contracts,message-state-types-contracts e2e-test-specific

.PHONY: test-provider-smoke
test-provider-smoke: ## Run explicit live provider smoke tests; tests skip unless matching AGENTSCOPE_TEST_* env vars are set
	@$(LOG_TARGET)
	$(MAKE) e2e-test-provider-smoke

.PHONY: docker-test-image
docker-test-image: ## Ensure the Docker test image exists locally
	@$(LOG_TARGET)
	docker image inspect "$(DOCKER_TEST_IMAGE)" >/dev/null 2>&1 || docker pull "$(DOCKER_TEST_IMAGE)"

.PHONY: test-e2e-docker
test-e2e-docker: ## Run Docker-backed E2E profile
test-e2e-docker: docker-test-image
	@$(LOG_TARGET)
	AGENTSCOPE_E2E_DOCKER=1 AGENTSCOPE_TEST_DOCKER=1 AGENTSCOPE_DOCKER_IMAGE="$(DOCKER_TEST_IMAGE)" $(MAKE) E2E_PROFILE=docker E2E_REPORT_DIR=e2e/reports/docker E2E_TIMEOUT=10m e2e-test

.PHONY: agent-sandbox-kind-setup
agent-sandbox-kind-setup: ## Create a KinD cluster and install Agent Sandbox test resources
	@$(LOG_TARGET)
	AGENT_SANDBOX_VERSION="$(AGENT_SANDBOX_VERSION)" \
	AGENT_SANDBOX_KIND_CLUSTER="$(AGENT_SANDBOX_KIND_CLUSTER)" \
	AGENT_SANDBOX_RUNTIME_IMAGE="$(AGENT_SANDBOX_RUNTIME_IMAGE)" \
	AGENTSCOPE_AGENT_SANDBOX_TEMPLATE="$(AGENTSCOPE_AGENT_SANDBOX_TEMPLATE)" \
	AGENTSCOPE_AGENT_SANDBOX_NAMESPACE="$(AGENTSCOPE_AGENT_SANDBOX_NAMESPACE)" \
	tools/agentsandbox/setup-kind.sh

.PHONY: agent-sandbox-kind-tools
agent-sandbox-kind-tools: ## Install kind and kubectl for Agent Sandbox E2E
	@$(LOG_TARGET)
	tools/agentsandbox/install-kind.sh

.PHONY: test-e2e-agent-sandbox
test-e2e-agent-sandbox: ## Run Agent Sandbox-backed E2E profile on KinD
test-e2e-agent-sandbox: agent-sandbox-kind-setup
	@$(LOG_TARGET)
	AGENTSCOPE_TEST_AGENT_SANDBOX=1 \
	AGENTSCOPE_E2E_AGENT_SANDBOX=1 \
	AGENTSCOPE_AGENT_SANDBOX_TEMPLATE="$(AGENTSCOPE_AGENT_SANDBOX_TEMPLATE)" \
	AGENTSCOPE_AGENT_SANDBOX_NAMESPACE="$(AGENTSCOPE_AGENT_SANDBOX_NAMESPACE)" \
	$(MAKE) E2E_PROFILE=agent-sandbox E2E_REPORT_DIR=e2e/reports/agent-sandbox E2E_TIMEOUT=20m e2e-test || { \
		status=$$?; \
		AGENT_SANDBOX_KIND_CLUSTER="$(AGENT_SANDBOX_KIND_CLUSTER)" \
		AGENTSCOPE_AGENT_SANDBOX_NAMESPACE="$(AGENTSCOPE_AGENT_SANDBOX_NAMESPACE)" \
		tools/agentsandbox/dump-kind-diagnostics.sh; \
		exit $$status; \
	}

.PHONY: ci
ci: ## Run CI-aligned checks for formatting, linting, spelling, security, and tests
ci: ci-tools fmt-check lint-go codespell-check markdown-lint security-check test

.PHONY: ci-e2e
ci-e2e: ## Run CI infrastructure E2E profiles
	@$(LOG_TARGET)
	$(MAKE) test-e2e-docker
	$(MAKE) agent-sandbox-kind-tools
	$(MAKE) test-e2e-agent-sandbox

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

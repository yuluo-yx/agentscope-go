##@ E2E

E2E_BIN ?= $(BUILD_DIR)/e2e
E2E_PROFILE ?= local
E2E_VERBOSE ?= true
E2E_PARALLEL ?= false
E2E_TIMEOUT ?= 5m
E2E_TESTS ?=
E2E_REPORT_DIR ?= e2e/reports/$(E2E_PROFILE)
E2E_TEST_ARGS = -profile=$(E2E_PROFILE) -verbose=$(E2E_VERBOSE) -parallel=$(E2E_PARALLEL) -timeout=$(E2E_TIMEOUT) -report-dir=$(E2E_REPORT_DIR) $(if $(E2E_TESTS),-tests=$(E2E_TESTS),)

.PHONY: e2e-deps
e2e-deps: ## Download E2E module dependencies
	@$(LOG_TARGET)
	cd e2e && $(GO) mod download

.PHONY: e2e-tidy
e2e-tidy: ## Tidy the independent E2E module
	@$(LOG_TARGET)
	cd e2e && $(GO) mod tidy

.PHONY: build-e2e
build-e2e: ## Build the E2E runner
	@$(LOG_TARGET)
	cd e2e && $(GO) build -o ../$(E2E_BIN) ./cmd/e2e

.PHONY: e2e-test
e2e-test: build-e2e ## Run an E2E profile with E2E_PROFILE, E2E_TESTS, and E2E_TIMEOUT
	@$(LOG_TARGET)
	./$(E2E_BIN) $(E2E_TEST_ARGS)

.PHONY: e2e-test-no-key
e2e-test-no-key: E2E_PROFILE := local
e2e-test-no-key: E2E_REPORT_DIR := e2e/reports/local
e2e-test-no-key: build-e2e ## Run local E2E with DashScope keys removed from the environment
	@$(LOG_TARGET)
	env -u DASHSCOPE_API_KEY -u AI_DASHSCOPE_API_KEY ./$(E2E_BIN) $(E2E_TEST_ARGS)

.PHONY: e2e-test-dashscope
e2e-test-dashscope: E2E_PROFILE := dashscope-live
e2e-test-dashscope: E2E_REPORT_DIR := e2e/reports/dashscope-live
e2e-test-dashscope: build-e2e ## Run live DashScope E2E; requires DASHSCOPE_API_KEY or AI_DASHSCOPE_API_KEY
	@$(LOG_TARGET)
	./$(E2E_BIN) $(E2E_TEST_ARGS)

.PHONY: e2e-test-provider-smoke
e2e-test-provider-smoke: E2E_PROFILE := provider-smoke
e2e-test-provider-smoke: E2E_REPORT_DIR := e2e/reports/provider-smoke
e2e-test-provider-smoke: E2E_TIMEOUT := 2m
e2e-test-provider-smoke: build-e2e ## Run explicit live provider smoke tests; skipped unless matching AGENTSCOPE_TEST_* vars are set
	@$(LOG_TARGET)
	./$(E2E_BIN) $(E2E_TEST_ARGS)

.PHONY: e2e-test-specific
e2e-test-specific: build-e2e ## Run selected E2E tests, for example E2E_TESTS=agent-tool-loop
	@$(LOG_TARGET)
	@test -n "$(E2E_TESTS)" || { echo "set E2E_TESTS to a comma-separated test list" >&2; exit 2; }
	./$(E2E_BIN) $(E2E_TEST_ARGS)

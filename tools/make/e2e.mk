##@ E2E

E2E_BIN ?= $(BUILD_DIR)/e2e
E2E_PROFILE ?= local
E2E_VERBOSE ?= true
E2E_PARALLEL ?= false
E2E_TIMEOUT ?= $(if $(filter provider-smoke,$(E2E_PROFILE)),2m,$(if $(filter docker,$(E2E_PROFILE)),10m,$(if $(filter agent-sandbox,$(E2E_PROFILE)),20m,5m)))
E2E_TESTS ?=
E2E_REPORT_DIR ?= e2e/reports/$(E2E_PROFILE)
E2E_TEST_ARGS = -profile=$(E2E_PROFILE) -verbose=$(E2E_VERBOSE) -parallel=$(E2E_PARALLEL) -timeout=$(E2E_TIMEOUT) -report-dir=$(E2E_REPORT_DIR) $(if $(E2E_TESTS),-tests=$(E2E_TESTS),)

.PHONY: e2e-deps
e2e-deps:
	@$(LOG_TARGET)
	cd e2e && $(GO) mod download

.PHONY: e2e-tidy
e2e-tidy:
	@$(LOG_TARGET)
	cd e2e && $(GO) mod tidy

.PHONY: build-e2e
build-e2e:
	@$(LOG_TARGET)
	cd e2e && $(GO) build -o ../$(E2E_BIN) ./cmd/e2e

.PHONY: e2e
e2e: build-e2e ## Run E2E. Set E2E_PROFILE=local|dashscope-live|provider-smoke|docker|agent-sandbox
	@$(LOG_TARGET)
	@set -e; \
	case "$(E2E_PROFILE)" in \
		local) \
			env -u DASHSCOPE_API_KEY -u AI_DASHSCOPE_API_KEY ./$(E2E_BIN) $(E2E_TEST_ARGS); \
			;; \
		docker) \
			docker image inspect "$(DOCKER_TEST_IMAGE)" >/dev/null 2>&1 || docker pull "$(DOCKER_TEST_IMAGE)"; \
			AGENTSCOPE_E2E_DOCKER=1 AGENTSCOPE_TEST_DOCKER=1 AGENTSCOPE_DOCKER_IMAGE="$(DOCKER_TEST_IMAGE)" ./$(E2E_BIN) $(E2E_TEST_ARGS); \
			;; \
		agent-sandbox) \
			AGENT_SANDBOX_VERSION="$(AGENT_SANDBOX_VERSION)" \
			AGENT_SANDBOX_KIND_CLUSTER="$(AGENT_SANDBOX_KIND_CLUSTER)" \
			AGENT_SANDBOX_RUNTIME_IMAGE="$(AGENT_SANDBOX_RUNTIME_IMAGE)" \
			AGENTSCOPE_AGENT_SANDBOX_TEMPLATE="$(AGENTSCOPE_AGENT_SANDBOX_TEMPLATE)" \
			AGENTSCOPE_AGENT_SANDBOX_NAMESPACE="$(AGENTSCOPE_AGENT_SANDBOX_NAMESPACE)" \
			tools/agentsandbox/setup-kind.sh; \
			AGENTSCOPE_TEST_AGENT_SANDBOX=1 \
			AGENTSCOPE_E2E_AGENT_SANDBOX=1 \
			AGENTSCOPE_AGENT_SANDBOX_TEMPLATE="$(AGENTSCOPE_AGENT_SANDBOX_TEMPLATE)" \
			AGENTSCOPE_AGENT_SANDBOX_NAMESPACE="$(AGENTSCOPE_AGENT_SANDBOX_NAMESPACE)" \
			./$(E2E_BIN) $(E2E_TEST_ARGS) || { \
				status=$$?; \
				AGENT_SANDBOX_KIND_CLUSTER="$(AGENT_SANDBOX_KIND_CLUSTER)" \
				AGENTSCOPE_AGENT_SANDBOX_NAMESPACE="$(AGENTSCOPE_AGENT_SANDBOX_NAMESPACE)" \
				tools/agentsandbox/dump-kind-diagnostics.sh; \
				exit $$status; \
			}; \
			;; \
		*) \
			./$(E2E_BIN) $(E2E_TEST_ARGS); \
			;; \
	esac

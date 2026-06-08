SHELL:=/bin/bash

DATETIME = $(shell date +"%Y%m%d%H%M%S")

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DIR := bin
GO := go
HELP_NO_COLOR ?=

# Log the running target
LOG_TARGET = echo -e "\033[0;32m==================> Running $@ ============> ... \033[0m"
# Log debugging info

define log
echo -e "\033[36m==================>$1\033[0m"
endef
# Log error info

define errorLog
echo -e "\033[0;31m==================>$1\033[0m"
endef

HELP_TITLE := \033[1;3;34m
HELP_BOLD := \033[1m
HELP_CYAN := \033[36m
HELP_RESET := \033[0m
ifeq ($(HELP_NO_COLOR),1)
HELP_TITLE :=
HELP_BOLD :=
HELP_CYAN :=
HELP_RESET :=
endif

.PHONY: help
help:
	@awk \
		-v title="$(HELP_TITLE)" \
		-v bold="$(HELP_BOLD)" \
		-v cyan="$(HELP_CYAN)" \
		-v reset="$(HELP_RESET)" '\
		/^##@/ { current = substr($$0, 5); next } \
		/^[a-zA-Z_0-9-]+:.*##/ { \
			target = $$1; \
			sub(/:.*/, "", target); \
			description = $$0; \
			sub(/^[^#]*##[[:space:]]*/, "", description); \
			if (current == "") { current = "General" } \
			total++; \
			targets[total] = target; \
			descriptions[total] = description; \
			groups[total] = current; \
			if (!(current in seenGroups)) { \
				groupTotal++; \
				groupNames[groupTotal] = current; \
				seenGroups[current] = 1; \
			} \
			if (length(target) > width) { width = length(target) } \
		} \
		END { \
			print title "AgentScope Go - A Go framework for building agent-oriented LLM applications 🤖" reset; \
			print ""; \
			print "Usage:"; \
			print "  make " cyan "<target>" reset " " cyan "[VAR=value ...]" reset; \
			print ""; \
			print "Common targets:"; \
			printf "  make %s%-*s%s  %s\n", cyan, width, "ci", reset, "Run local CI-aligned checks"; \
			printf "  make %s%-*s%s  %s\n", cyan, width, "test", reset, "Run unit tests and the local E2E profile"; \
			printf "  make %s%-*s%s  %s\n", cyan, width, "e2e-test-no-key", reset, "Run deterministic local E2E without live provider keys"; \
			printf "  make %s%-*s%s  %s\n", cyan, width, "e2e-test-dashscope", reset, "Run live DashScope E2E"; \
			print ""; \
			print "GitHub workflow targets:"; \
			printf "  make %s%-*s%s  %s\n", cyan, width, "ci", reset, "Run formatting, linting, security checks, unit tests, and local E2E"; \
			printf "  make %s%-*s%s  %s\n", cyan, width, "ci-e2e", reset, "Run Docker and Agent Sandbox E2E"; \
			printf "  make %s%-*s%s  %s\n", cyan, width, "download", reset, "Download Go module dependencies"; \
			print ""; \
			print "Targets:"; \
			for (groupIndex = 1; groupIndex <= groupTotal; groupIndex++) { \
				group = groupNames[groupIndex]; \
				print ""; \
				print bold group reset; \
				orderedCount = 0; \
				for (targetIndex = 1; targetIndex <= total; targetIndex++) { \
					if (groups[targetIndex] == group) { \
						orderedCount++; \
						ordered[orderedCount] = targetIndex; \
					} \
				} \
				for (left = 1; left < orderedCount; left++) { \
					for (right = left + 1; right <= orderedCount; right++) { \
						leftTarget = targets[ordered[left]]; \
						rightTarget = targets[ordered[right]]; \
						if (length(rightTarget) < length(leftTarget) || (length(rightTarget) == length(leftTarget) && rightTarget < leftTarget)) { \
							tmp = ordered[left]; \
							ordered[left] = ordered[right]; \
							ordered[right] = tmp; \
						} \
					} \
				} \
				for (orderedIndex = 1; orderedIndex <= orderedCount; orderedIndex++) { \
					targetIndex = ordered[orderedIndex]; \
					printf "  %s%-*s%s  %s\n", cyan, width, targets[targetIndex], reset, descriptions[targetIndex]; \
				} \
				for (orderedIndex = 1; orderedIndex <= orderedCount; orderedIndex++) { \
					delete ordered[orderedIndex]; \
				} \
				delete ordered[0]; \
			} \
		}' $(MAKEFILE_LIST)

##@ Documentation

DOCS_DIR ?= docs
DOCS_BUILD_DIR ?= $(DOCS_DIR)/_build
DOCS_HOST ?= 127.0.0.1
DOCS_PORT ?= 8000
JUPYTER_BOOK := $(PYTHON_VENV_BIN)/jupyter-book

.PHONY: install-docs
install-docs: ## Install documentation build dependencies
install-docs: python-venv
	@$(LOG_TARGET)
	"$(PYTHON_VENV_BIN)/python" -m pip install -e "$(DOCS_DIR)[dev]"
	@"$(JUPYTER_BOOK)" --version

.PHONY: docs-build
docs-build: ## Build the Jupyter Book documentation site
docs-build: install-docs
	@$(LOG_TARGET)
	"$(JUPYTER_BOOK)" build "$(DOCS_DIR)"

.PHONY: docs-clean
docs-clean: ## Clean documentation build artifacts
docs-clean: install-docs
	@$(LOG_TARGET)
	"$(JUPYTER_BOOK)" clean "$(DOCS_DIR)" --all

.PHONY: docs-lint
docs-lint: ## Lint documentation markdown files
docs-lint: install-markdownlint
	@$(LOG_TARGET)
	"$(MARKDOWNLINT)" --version
	"$(MARKDOWNLINT)" --config ./tools/linter/markdownlint/markdown_lint_config.yml --ignore tools/node/node_modules "$(DOCS_DIR)"

.PHONY: docs-check
docs-check: ## Lint and build documentation
docs-check: docs-lint docs-build

.PHONY: docs-serve
docs-serve: ## Serve built documentation locally
docs-serve: docs-build
	@$(LOG_TARGET)
	"$(PYTHON_VENV_BIN)/python" -m http.server "$(DOCS_PORT)" --bind "$(DOCS_HOST)" --directory "$(DOCS_BUILD_DIR)/html"

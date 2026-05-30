_run:
	@$(MAKE) --warn-undefined-variables \
		-f tools/make/common.mk \
		-f tools/make/tools.mk \
		-f tools/make/linter.mk \
		-f tools/make/go.mk \
		$(MAKECMDGOALS)

.PHONY: _run

$(if $(MAKECMDGOALS),$(MAKECMDGOALS): %: _run)

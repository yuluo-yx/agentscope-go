# Test Layout

Project home: [README.md](../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

The `test` directory is organized by test scope:

| Directory | Purpose |
| --- | --- |
| `architecture/` | Static architecture and package-structure checks. |
| `integration/` | Component integration tests that exercise multiple packages without full user workflows. |
| `e2e/` | End-to-end framework workflows that drive agents, models, tools, workspace, and events together. |

## Commands

Run the regular local test suite:

```bash
make test
```

Run integration tests only:

```bash
make test-integration
```

Run E2E tests only:

```bash
make test-e2e
```

Run Docker-backed E2E and integration tests:

```bash
make test-e2e-docker
```

`test-e2e-docker` uses `DOCKER_TEST_IMAGE`, defaulting to `ubuntu:latest`. It sets `AGENTSCOPE_E2E_DOCKER=1`, `AGENTSCOPE_TEST_DOCKER=1`, and `AGENTSCOPE_DOCKER_IMAGE` for the Go tests.

# Agent Context Strategy Example

This example demonstrates:

- enabling summary compression with `agent.ContextConfig.MaxTokens`;
- offloading compressed messages through a local workspace;
- replacing the default context strategy chain with `agent.WithContextStrategies`.

## Run

```bash
go run .
```

Expected output:

```text
summary=true remaining=1 offloaded=true custom_summary="summary from a custom context strategy" model_calls=1
```

The exact temporary workspace path is not printed, but the example verifies that the context JSONL file was written under the local workspace session directory.

# Event Runner Loop Example

This example shows how `loop/automation/event`, `loop/automation/runner`, and `loop/automation/store` turn a generic event into a loop-enabled Agent run.

It uses a local scripted model, so no model provider credentials are required.

Run:

```bash
go run .
```

The example constructs an `event.Event`, routes it to one Agent, maps the event into a user message with `runner.TemplateMapper`, runs `Agent.ReplyStream`, and records the run in `store.MemoryRunStore`.

Concept mapping:

- `event.Event` is the automation heartbeat: an external signal discovers and triggers work.
- `runner.TemplateMapper` is the system prompting the Agent for you: events become stable Agent input instead of repeated hand-written prompts.
- `store.MemoryRunStore` is cross-run state: it records events and runs instead of relying only on one conversation context.
- The example stays report-only. It does not create pull requests or modify external systems, which keeps the default production boundary conservative.

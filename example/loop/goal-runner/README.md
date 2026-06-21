# Goal Runner Loop Example

This example shows how `loop/automation/goal.GoalRunner` starts a second Agent run after the verifier rejects the first attempt, maps `NextAction` into the next user message, and stops after the second verifier pass.

It uses a DashScope ChatModel, so set `AI_DASHSCOPE_API_KEY` before running it.

Run:

```bash
go run .
```

Expected output shows:

- `completed=true`.
- `attempts=2`.
- the first run stops with `verifier_failed`.
- the second run stops with `completed`.

The flow demonstrates cross-run goal continuation: initial input starts the first attempt, the verifier returns failure plus `NextAction`, `TemplateNextActionMapper` turns that feedback into the next user message, and the second attempt passes after adding evidence.

Concept mapping:

- `goal.GoalRunner` maps to `/goal`: the loop keeps going until a verifiable stop condition holds.
- The DashScope-backed Agent is the maker and `core.Verifier` is the checker. The Agent that writes the result does not grade its own completion state.
- `goal.TemplateNextActionMapper` turns checker feedback into the next Agent input, so the system prompts the Agent instead of a human doing every turn.
- `store.MemoryRunStore` records each run and report, modeling the external state a long-running loop needs.

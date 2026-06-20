# Loop Engineering

The `loop` package provides framework APIs for building Loop Engineering agents. It does not replace the ReAct execution loop in the `agent` package. Instead, it adds goals, success criteria, budgets, verification, state, and events around an Agent run.

Loop Engineering turns repeated manual prompting into a reusable control loop: intent, context, action, observation, adjustment, and stop conditions. AgentScope Go provides these building blocks while scheduling, ticket systems, pull request policy, and auto-merge rules remain application responsibilities.

## When to Use It

Use `loop` when an application needs to:

- constrain an Agent run with a clear goal and success criteria;
- track model calls, tool calls, token usage, and stop reasons;
- separate the implementer from the verifier;
- publish lifecycle events to UI, logs, audit sinks, or run logs;
- roll out from report-only to assisted or unattended operation.

For a regular multi-turn tool workflow, use `agent.Agent` and `agent.WithReActConfig` directly.

## Core Concepts

### Spec

`loop.Spec` is the public contract for a loop design:

```go
spec := loop.Spec{
	Name: "daily-triage",
	Goal: "scan repository signals and produce a report without modifying code",
	NonGoals: []string{
		"create pull requests",
		"merge code",
	},
	SuccessCriteria: []loop.SuccessCriterion{
		{Name: "report", Description: "final reply lists findings and next action", Required: true},
	},
	Mode:   loop.ModeReportOnly,
	Policy: loop.DefaultPolicy(loop.ModeReportOnly),
}
```

### Mode

`loop.Mode` describes the autonomy level:

| Mode | Meaning |
| --- | --- |
| `loop.ModeReportOnly` | Report and record state, but do not act autonomously. |
| `loop.ModeAssisted` | Allow bounded action, usually with verifier or human review. |
| `loop.ModeUnattended` | Allow unattended operation. Requires verifier, budget, and human gates. |

### Policy

`loop.Policy` bounds iterations, model calls, tool calls, token usage, and attempts. When the budget is exhausted, the middleware injects a wrap-up hint before the next reasoning pass and sets `tool_choice=none` on the model request.

### Verifier

`loop.Verifier` supports maker/checker separation. The Agent that produced the work should not be the only judge of completion. A verifier can wrap tests, rules, another Agent, CI results, MCP tools, or business system checks.

```go
verifier := loop.VerifierFunc(func(ctx context.Context, input loop.VerificationInput) (loop.VerificationResult, error) {
	return loop.VerificationResult{
		Passed:   true,
		Reason:   "local checks passed",
		Evidence: []string{"go test ./..."},
	}, nil
})
```

## Agent Integration

Use `loop.WithSpec` when constructing an Agent:

```go
agent, err := agent.NewAgent(
	"Friday",
	"You are concise.",
	chatModel,
	loop.WithSpec(spec, loop.WithVerifier(verifier)),
)
```

The middleware participates in these hooks:

- `SystemPromptMiddleware`: injects loop goals, non-goals, success criteria, and human gates.
- `ReplyMiddleware`: initializes `state.LoopContext`, records metrics, and emits lifecycle events.
- `ReasoningMiddleware`: emits iteration events and injects wrap-up hints when a budget is exhausted.
- `ModelCallMiddleware`: forces `tool_choice=none` during wrap-up.
- `ActingMiddleware`: preserves the tool execution chain without hiding business actions in middleware.

## State and Events

When `loop.WithSpec` is enabled, the Agent state keeps `state.LoopContext`. It records loop name, goal, mode, iterations, model calls, tool calls, token usage, latest verifier result, and stop reason.

Loop events use existing `message.CustomEvent` values:

| Event | Meaning |
| --- | --- |
| `loop.start` | A loop run started. |
| `loop.iteration_start` | A reasoning iteration started. |
| `loop.iteration_end` | A reasoning iteration ended. |
| `loop.verify_start` | Verification started. |
| `loop.verify_end` | Verification finished. |
| `loop.wrap_up` | The loop entered wrap-up. |
| `loop.stop` | A loop run stopped. |

Existing SSE and AG-UI conversion middleware preserves these events as custom events.

## Boundaries

The `loop` package does not include:

- cron, schedulers, or always-on workers;
- Git worktree creation and cleanup;
- GitHub PR creation, comments, or merges;
- Jira, Linear, Slack, DingTalk, or other business connectors;
- a fixed `STATE.md` schema;
- auto-merge or release policy.

Applications can compose those capabilities with `workspace`, `team`, `tool/mcp`, `tool/task`, and business code.

## Examples

- `example/loop/basic`: report-only loop with state and events.
- `example/loop/assisted-verifier`: assisted loop with verifier-based maker/checker separation.

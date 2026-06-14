# Agent Team Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows process-local Agent team coordination:

- Create a process-local team manager with the standalone `team` package.
- Attach team tools to a leader with `team.WithTeam`.
- Let the leader call `TeamCreate` and `AgentCreate`.
- Deliver the worker's first task through the team inbox.
- Let the worker report back with `TeamSay`.

## Prerequisites

- Go 1.26.3.
- No API key is required.

## Run

```bash
cd example/agent/team
go run .
```

## Expected Output

Output includes:

```text
team=Launch members=1
```

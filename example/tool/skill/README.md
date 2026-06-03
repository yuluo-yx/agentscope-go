# Skill Through Agent Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows an end-to-end flow where an Agent uses local skills as tools:

- Load skills from `resources/**/SKILL.md` with `tool/skill.LocalLoader`.
- Wrap each loaded skill as a `tool.FunctionTool` (`Skill_<name>`).
- Register those tools in `tool.Toolkit`.
- Run `agent.ReplyStream` with a local scripted model that emits a skill tool call.

## Prerequisites

- Go 1.26.3.
- No API key is required.

## Run

```bash
cd example/tool/skill
go run .
```

## Expected Output

Output includes:

```text
skills=2 names=code-review,planning agent_reply=I used Skill_planning and summarized its guidance. events=tool_call:Skill_planning,tool_result:success
```

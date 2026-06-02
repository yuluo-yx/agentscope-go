# 通过 Agent 使用 Skill 示例

英文文档见 [README.md](README.md)。

这个示例演示一个完整闭环：把本地 `SKILL.md` 加载成工具，再由 Agent 调用。

- 使用 `tool/skill.LocalLoader` 加载 `resources/**/SKILL.md`。
- 把每个 skill 包装成 `tool.FunctionTool`（命名为 `Skill_<name>`）。
- 把这些工具注册到 `tool.Toolkit`。
- 使用本地 scripted model 产出一次 skill 的 tool call，并通过 `agent.ReplyStream` 执行。

## 前置条件

- Go 1.26.3。
- 不需要 API Key。

## 运行

```bash
cd example/tool/skill
go run .
```

## 预期输出

输出包含：

```text
skills=2 names=code-review,planning agent_reply=I used Skill_planning and summarized its guidance. events=tool_call:Skill_planning,tool_result:success
```

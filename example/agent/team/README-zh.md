# Agent Team 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示进程内 Agent team 协作：

- 通过独立的 `team` 包创建进程内 team manager。
- 通过 `team.WithTeam` 给 leader 挂载 team tools。
- 使用 DashScope ChatModel 的 leader 调用 `TeamCreate` 和 `AgentCreate`。
- worker 的初始任务通过 team inbox 投递。
- worker 通过 `TeamSay` 汇报结果。

## 前置条件

- Go 1.26.3。
- DashScope ChatModel 需要 `AI_DASHSCOPE_API_KEY`。

## 运行

```bash
cd example/agent/team
go run .
```

## 预期输出

输出包含：

```text
team=Launch members=1
```

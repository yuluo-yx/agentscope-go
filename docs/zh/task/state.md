# 状态管理

`state.AgentState` 保存智能体和工具共享的运行期信息。

## 字段

| 字段 | 用途 |
| --- | --- |
| `SessionID` | 稳定会话标识 |
| `Context` | 对话消息 |
| `PermissionContext` | 权限模式、工作目录和规则 |
| `ToolContext` | 文件读取缓存和已启用工具组 |
| `TaskContext` | 任务工具使用的任务列表 |

## 创建状态

```go
agentState := state.NewAgentState()
```

## 任务上下文

```go
task := state.NewTask("Write docs", "Create the MCP guide.", map[string]any{
	"area": "docs",
})
agentState.TaskContext.AddTask(task)
_ = agentState.TaskContext.UpdateTaskState(task.ID, state.TaskInProgress)
```

## 克隆

调用方需要隔离副本时使用 `Clone`：

```go
copy := agentState.Clone()
```

克隆会深拷贝消息、权限规则、工具缓存条目和任务。

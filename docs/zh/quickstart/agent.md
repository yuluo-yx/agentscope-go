# 构建智能体

本页展示最小智能体的组成方式。需要可直接运行的版本时，请优先参考 `example/` 下的示例目录。

## 创建聊天模型

```go
chat, err := dashscope.NewChatModel(
	dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
	"qwen3.7-max",
	dashscope.WithStream(false),
)
if err != nil {
	panic(err)
}
```

## 创建工具

```go
greet, err := tool.NewFunctionTool(
	"Greet",
	"Return a greeting for one name.",
	map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	},
	func(_ context.Context, input map[string]any, _ *state.AgentState) (message.ContentBlockList, error) {
		name, _ := input["name"].(string)
		return message.ContentBlockList{message.NewTextBlock("hello " + name)}, nil
	},
	tool.WithFunctionReadOnly(true),
)
if err != nil {
	panic(err)
}
```

## 注册工具

```go
kit, err := tool.NewToolkit(greet)
if err != nil {
	panic(err)
}
```

## 运行智能体循环

`agent` 包可以驱动完整循环。需要手动控制时，可以把工具 Schema 发给模型，使用 `Toolkit` 执行模型返回的工具调用，再把 `ToolResultBlock` 发回模型。

```go
schemas, err := kit.ToolSchemas()
if err != nil {
	panic(err)
}

user, err := message.NewUserMessage("user", "Use the Greet tool to greet Go.")
if err != nil {
	panic(err)
}

response, err := chat.Call(context.Background(), model.CallRequest{
	Messages: []*message.Message{user},
	Tools:    schemas,
})
if err != nil {
	panic(err)
}
```

## 可运行示例

- `example/agent/basic`：使用脚本模型演示端到端 ReAct 流程。
- `example/tool/function`：演示函数工具和可选 DashScope 工具调用循环。
- `example/tool/mcp`：演示 MCP 工具通过 `tool.Toolkit` 注册和执行。

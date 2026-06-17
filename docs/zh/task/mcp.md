# 模型上下文协议

`tool/mcp` 把 Model Context Protocol 服务连接到 AgentScope Go 工具系统。

## 提供能力

- Stdio MCP 客户端。
- HTTP MCP 客户端，支持 SSE 或 Streamable HTTP 传输选择。
- 用于本地示例和测试的进程内 MCP 客户端。
- 有状态和无状态连接模式。
- 工具启用和禁用过滤。
- MCP 工具包装为 AgentScope `tool.Tool`。
- MCP 内容转换为 `message.TextBlock` 和 `message.DataBlock`。
- `DeferredToolkit` 延迟加载 MCP 工具 Schema，首次读取 Schema、查找工具或执行工具时才调用 `ListTools`。
- `WithTaskTTL` 会在 MCP `tools/call` 请求中写入标准 `task` 参数，让支持 task augmentation 的服务端按任务方式执行。

## 工具命名

MCP 工具使用与 Python 实现一致的命名规则：

```text
mcp__<server>__<tool>
```

例如，客户端名称为 `people`，原始工具名为 `lookup_profile`，暴露后的工具名是：

```text
mcp__people__lookup_profile
```

## 进程内示例

```go
client, err := mcp.NewInProcessClient("people", server)
if err != nil {
	panic(err)
}
if err := client.Connect(ctx); err != nil {
	panic(err)
}
defer client.Close()

tools, err := client.ListTools(ctx)
if err != nil {
	panic(err)
}

kit, err := tool.NewToolkit(tools...)
```

## 延迟加载

需要避免启动时立即拉取 MCP 工具列表时，可以使用 `DeferredToolkit`：

```go
kit, err := mcp.NewDeferredToolkit(client)
if err != nil {
	panic(err)
}

schemas, err := kit.ToolSchemas()
```

`DeferredToolkit` 会缓存包装后的工具。收到 MCP `tools/list_changed` 通知或应用层确认工具集变化后，可以调用
`kit.Invalidate()`，下一次读取 Schema、查找工具或执行工具时会重新加载。

## Task augmentation

需要让支持 task augmentation 的 MCP 服务端按任务方式处理工具调用时，创建客户端时传入 TTL：

```go
client, err := mcp.NewHTTPClient(
	"tasks",
	mcp.HTTPConfig{URL: "https://example.com/mcp"},
	mcp.WithTaskTTL(5*time.Minute),
)
```

`WithTaskTTL` 会把 TTL 转成毫秒并写入 `CallToolRequest.Params.Task`。如果服务端工具声明
`TaskSupportOptional` 或 `TaskSupportRequired`，服务端可以据此走任务执行路径；普通工具仍按常规工具调用返回。

## 权限行为

如果 MCP 工具声明 `readOnlyHint=true`，AgentScope Go 默认允许执行。其他 MCP 工具默认需要询问。

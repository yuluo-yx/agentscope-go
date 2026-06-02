# 消息

消息是用户、模型、工具和智能体之间的协议。

## 角色

常见角色可以使用构造函数创建：

```go
system, err := message.NewSystemMessage("system", "You are concise.")
user, err := message.NewUserMessage("user", "Create a plan.")
assistant, err := message.NewAssistantMessage("assistant", "Use a workspace and call tools.")
```

`NewSystemMessage` 会立即标记完成，因为系统提示词在创建时已经确定。`NewAssistantMessage` 表示模型输出，不会强制写入完成时间。

## 内容块

文本：

```go
block := message.NewTextBlock("hello")
```

数据：

```go
image := message.NewDataBlock(
	message.NewBase64Source("base64-data", "image/png"),
	message.WithDataBlockName("diagram.png"),
)
```

工具结果：

```go
result := message.NewToolResultBlock(
	"call-1",
	"Read",
	message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("content")}},
)
```

`NewToolResultBlock` 默认状态是 `ToolResultRunning`，与 Python 的 `ToolResultBlock.state` 默认值一致。构造已经完成的工具结果并回填给模型时，再显式传入 `message.ToolResultSuccess` 等真实状态。

需要从内容块列表中读取文本或筛选内容块时，可以使用统一的查询方法：

```go
blocks := message.ContentBlockList{
	message.NewTextBlock("hello"),
	message.NewToolCallBlock("call-1", "Read", `{"path":"README.md"}`),
	message.NewTextBlock("world"),
}

text := blocks.GetTextContent()
if text != nil {
	fmt.Println(*text) // hello
	// world
}

joined := blocks.GetTextContent(" ")
if joined != nil {
	fmt.Println(*joined) // hello world
}

toolCalls := blocks.GetContentBlocks("tool_call")
if len(toolCalls) > 0 {
	fmt.Println(toolCalls[0].BlockID())
}
```

`GetTextContent()` 与 Python API 的默认语义一致，多个文本块会用换行符拼接；只有需要自定义拼接格式时才传入分隔符。

## 对话历史

模型可用的历史消息通常保存为切片：

```go
history := []*message.Message{system, user, assistant}
```

`message` 包会在需要时克隆内容块，调用方可以继续持有自己的输入数据。

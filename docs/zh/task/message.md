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
	message.ToolResultSuccess,
)
```

## 对话历史

模型可用的历史消息通常保存为切片：

```go
history := []*message.Message{system, user, assistant}
```

`message` 包会在需要时克隆内容块，调用方可以继续持有自己的输入数据。

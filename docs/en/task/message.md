# Messages

Messages are the protocol between users, models, tools, and agents.

## Roles

Use constructor helpers for common roles:

```go
system, err := message.NewSystemMessage("system", "You are concise.")
user, err := message.NewUserMessage("user", "Create a plan.")
assistant, err := message.NewAssistantMessage("assistant", "Use a workspace and call tools.")
```

`NewSystemMessage` finishes immediately because a system prompt is known at creation time. `NewAssistantMessage` represents model output and does not force a finish timestamp.

## Content Blocks

Text:

```go
block := message.NewTextBlock("hello")
```

Data:

```go
image := message.NewDataBlock(
	message.NewBase64Source("base64-data", "image/png"),
	message.WithDataBlockName("diagram.png"),
)
```

Tool result:

```go
result := message.NewToolResultBlock(
	"call-1",
	"Read",
	message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("content")}},
	message.ToolResultSuccess,
)
```

## Conversation History

Keep model-ready history as a slice of messages:

```go
history := []*message.Message{system, user, assistant}
```

The `message` package clones content blocks when needed so callers can keep ownership of their input data.

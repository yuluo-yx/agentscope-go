# Message Example

Chinese documentation: [README-zh.md](README-zh.md).

This example demonstrates the `message` package:

- Build a conversation history with system, user, and assistant messages.
- Read role order and text content from the history.
- Show that system messages are finished immediately while assistant messages can represent model output.

## Prerequisites

- Go 1.26.3.
- No API key is required.

## Run

```bash
cd example/message
go run .
```

## Expected Output

Output includes:

```text
conversation_messages=3
```

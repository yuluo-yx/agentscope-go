# Built-in Tool Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example demonstrates the current built-in tools:

- `Bash`: run a safe shell command.
- `Write`: write a file.
- `Read`: read a file and populate read cache.
- `Edit`: replace text in a previously read file.
- `Glob`: find files by glob.
- `Grep`: search file contents.
- Let DashScope ChatModel request the `Read` tool against the temporary file, execute it locally, send a `ToolResultBlock` back, and print the final model response.

The example only writes and edits files in a temporary directory and cleans it up when finished.

## Prerequisites

- Go 1.26.3.
- A local shell must be available.
- `AI_DASHSCOPE_API_KEY` for the DashScope model -> tool call -> tool result loop.

## Run

```bash
cd example/tool/builtin
go run .
```

## Expected Output

Output includes:

```text
builtin_tools=Bash,Edit,Glob,Grep,Read,Write
chat_tool=Read
```

# Local Workspace Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example demonstrates `workspace/local.Workspace`:

- Initialize a local workspace directory.
- Get built-in `Write` and `Read` tools from the workspace.
- Use those tools to write and read a brief inside the workspace `data/` directory.
- Copy and load a local skill.
- Offload context messages to JSONL.
- Offload the tool result to a text file.
- Convert base64 data blocks to file URLs inside the workspace.

## Prerequisites

- Go 1.26.3.
- No API key is required.

## Run

```bash
cd example/workspace/local
go run .
```

## Expected Output

Output includes:

```text
workspace_alive=true
read_has_brief=true
```

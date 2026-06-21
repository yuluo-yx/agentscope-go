# 内置工具示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示当前内置工具包：

- `Bash`：执行安全 shell 命令。
- `Write`：写入文件。
- `Read`：读取文件并写入 read cache。
- `Edit`：在已读取文件上做字符串替换。
- `Glob`：按 glob 查找文件。
- `Grep`：按正则或文本搜索文件内容。
- 让 DashScope ChatModel 请求读取临时文件，本地执行 `Read` 工具，把 `ToolResultBlock` 回填给模型，并输出最终模型回复。

示例只在临时目录内写入和修改文件，运行结束会清理临时目录。

## 前置条件

- Go 1.26.3。
- 本机需要可用 shell。
- DashScope model -> tool call -> tool result 闭环需要 `AI_DASHSCOPE_API_KEY`。

## 运行

```bash
cd example/tool/builtin
go run .
```

## 预期输出

输出包含：

```text
builtin_tools=Bash,Edit,Glob,Grep,Read,Write
chat_tool=Read
```

# 本地 Workspace 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示 `workspace.LocalWorkspace`：

- 初始化本地 workspace 目录。
- 从 workspace 获取内置 `Write` 和 `Read` 工具。
- 使用这些工具在 workspace 的 `data/` 目录中写入并读取 brief 文件。
- 复制并加载本地 skill。
- 将上下文消息 offload 为 JSONL。
- 将真实工具结果 offload 为文本文件。
- 将 base64 data block 转换为 workspace 内的 file URL。

## 前置条件

- Go 1.26.3。
- 不需要 API Key。

## 运行

```bash
cd example/workspace/local
go run .
```

## 预期输出

输出包含：

```text
workspace_alive=true
read_has_brief=true
```

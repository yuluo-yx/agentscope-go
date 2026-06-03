# 消息示例

项目主页：[README-zh.md](../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示 `message` 包的基本用法：

- 用 system、user、assistant 消息组织一段对话历史。
- 从对话历史中读取角色顺序和文本内容。
- 展示 system 消息会立即完成，而 assistant 消息可表示模型输出。

## 前置条件

- Go 1.26.3。
- 不需要 API Key。

## 运行

```bash
cd example/message
go run .
```

## 预期输出

输出包含：

```text
conversation_messages=3
```

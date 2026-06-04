# Agent Context Strategy 示例

本示例演示：

- 使用 `agent.ContextConfig.MaxTokens` 启用摘要压缩；
- 通过本地 workspace 卸载被压缩的旧消息；
- 使用 `agent.WithContextStrategies` 替换默认上下文策略链。

## 运行

```bash
go run .
```

预期输出：

```text
summary=true remaining=1 offloaded=true custom_summary="summary from a custom context strategy" model_calls=1
```

示例不会打印临时 workspace 的完整路径，但会验证 context JSONL 文件已经写入本地 workspace 的 session 目录。

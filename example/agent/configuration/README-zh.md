# Agent 配置示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

这个示例展示常见 Agent 配置点：

- `WithModelConfig` 设置重试次数和 fallback model。
- `WithContextConfig` 控制上下文清理时本地工具结果截断。
- `WithReActConfig` 设置最大 reasoning/action 循环次数。

主脚本模型会故意失败。Agent 按 `ModelConfig` 重试后使用 fallback model。示例还会在 state 中预置一段很长的工具结果，让上下文清理在模型调用前截断它。

## 前置条件

- Go 1.26.3 或更新版本。
- 不需要 API Key。

## 运行

```bash
cd example/agent/configuration
go run .
```

预期输出：

```text
reply=fallback model replied primary_stream_calls=1 fallback_stream_calls=1 compressed=true
```

## 测试

```bash
go test .
```

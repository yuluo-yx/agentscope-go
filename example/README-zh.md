# AgentScope Go 示例

项目主页：[README-zh.md](../README-zh.md)。

英文文档：[README.md](README.md)。

本目录按功能模块组织示例。每个子目录都是独立 Go 项目，包含自己的 `go.mod`、`main.go`、`README.md` 和 `README-zh.md`，可以单独进入目录运行。

## 运行示例

每个子目录都是独立 Go module。进入要体验的目录后运行：

```bash
go run .
```

模型相关示例会按 provider 分目录展示 `ChatModel`、Embedding、TTS 和 STT 的真实调用方式。多数 ChatModel 示例会演示完整的模型工具闭环：模型生成工具调用、本地执行工具、把工具结果回填给模型，再取得最终回复。

这些模型示例默认会发起真实服务请求。运行前需要设置对应 provider 的 API Key；Ollama 示例需要本地 Ollama 服务和已拉取的模型。

## 示例列表

| 目录 | 功能 |
| --- | --- |
| `message` | system、user、assistant 消息组成的对话历史 |
| `model/anthropic/chat` | Anthropic ChatModel 非流式、流式、token 估算和工具调用闭环 |
| `model/dashscope/chat` | DashScope OpenAI-compatible ChatModel、多模态消息、token 估算和工具调用闭环 |
| `model/dashscope/embedding` | DashScope 文本向量模型，覆盖多输入 embedding 和维度配置 |
| `model/dashscope/stt` | DashScope 语音识别模型，读取本地 WAV 文件并输出识别文本 |
| `model/dashscope/tts` | DashScope 语音合成模型，流式接收音频块并写入 `output.wav` |
| `model/deepseek/chat` | DeepSeek ChatModel 非流式、流式和工具调用闭环 |
| `model/gemini/chat` | Gemini ChatModel 多模态消息、token 估算、非流式、流式和工具调用闭环 |
| `model/moonshot/chat` | Moonshot ChatModel 多模态消息、token 估算、非流式、流式和工具调用闭环 |
| `model/ollama/chat` | 本地 Ollama ChatModel 非流式、流式和工具调用闭环 |
| `model/openai/chat` | OpenAI ChatModel 非流式、流式、代理 HTTP client 和工具调用闭环 |
| `model/xai/chat` | xAI ChatModel 多模态消息、token 估算、非流式、流式和工具调用闭环 |
| `model/zhipu/chat` | 智谱 AI ChatModel 非流式、流式、token 估算和工具调用闭环 |
| `agent/basic` | Agent + scripted model + task tool 的端到端 ReAct 流程 |
| `agent/team` | 进程内 leader/worker Agent team tools 与 inbox 投递 |
| `agent/configuration` | Agent model fallback、ReAct 配置和本地上下文清理 |
| `agent/context_strategy` | 摘要压缩、workspace offload 和自定义上下文策略 |
| `agent/external` | Agent 外部工具执行的暂停与恢复流程 |
| `agent/hooks` | Agent middleware hook 示例，覆盖 reply、reasoning、model call、acting 和 system prompt |
| `agent/middleware_tracing` | reply、model call 和 tool execution span 的 tracing middleware |
| `agent/permission` | Agent 权限确认与恢复流程 |
| `integration/gin` | Gin HTTP 集成，覆盖底层 ChatModel 流式与 Agent 事件流式 |
| `integration/kratos` | Kratos HTTP 集成，覆盖底层 ChatModel 流式与 Agent 事件流式 |
| `tool/function` | 自定义函数工具 |
| `tool/builtin` | Bash/Edit/Glob/Grep/Read/Write 内置工具 |
| `tool/mcp` | MCP client 集成、MCP tool 包装、Toolkit 执行和可选真实 ChatModel 工具调用 |
| `tool/task` | TaskCreate/TaskGet/TaskList/TaskUpdate |
| `tool/skill` | 本地 `SKILL.md` 加载 |
| `workspace/local` | workspace 支撑的工具文件操作、skills、上下文与工具结果 offload |
| `workspace/docker` | Docker workspace 工具、容器文件操作和可选 ChatModel 回复 |
| `workspace/microsandbox` | Microsandbox microVM workspace 工具和可选 DashScope ChatModel 回复 |
| `workspace/daytona` | Daytona 沙箱 workspace，在远端沙箱中执行 Python CSV 数据分析 |

## 外部服务

模型示例按 provider 使用不同环境变量。常用变量如下：

```bash
export AI_OPENAI_API_KEY=your-openai-key
export AI_ANTHROPIC_API_KEY=your-anthropic-key
export AI_DASHSCOPE_API_KEY=your-dashscope-key
export AI_DEEPSEEK_API_KEY=your-deepseek-key
export AI_GEMINI_API_KEY=your-gemini-key
export AI_MOONSHOT_API_KEY=your-moonshot-key
export AI_XAI_API_KEY=your-xai-key
export AI_ZHIPU_API_KEY=your-zhipu-key
```

OpenAI 示例额外支持 `AI_OPENAI_PROXY_URL`，用于本地代理访问。Ollama 示例使用 `http://127.0.0.1:11434`，运行前需要先启动 Ollama 并拉取 `llama3.1`。

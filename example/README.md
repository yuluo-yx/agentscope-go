# AgentScope Go Examples

Project home: [README.md](../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This directory organizes examples by feature area. Most runnable leaf directories are standalone Go modules with their own `go.mod`, `main.go`, `README.md`, and `README-zh.md`. A few root-module packages, such as `loop/event-runner` and `loop/goal-runner`, are tested through the repository root.

## Run Examples

For standalone example modules, enter the directory you want to try and run:

```bash
go run .
```

Model examples are split by provider and demonstrate live `ChatModel`, embedding, text-to-speech, and speech-to-text calls. Most ChatModel examples run the full model tool loop: the model requests a tool call, the local toolkit executes it, the tool result is sent back to the model, and the final answer is printed.

These model examples make real provider requests by default. Set the matching provider API key before running them; the Ollama example requires a local Ollama service and a pulled model.

## Example List

| Directory | Feature |
| --- | --- |
| `message` | Conversation history with system, user, and assistant messages |
| `model/anthropic/chat` | Anthropic ChatModel non-streaming call, streaming call, token estimation, and tool loop |
| `model/dashscope/chat` | DashScope OpenAI-compatible ChatModel, multimodal message, token estimation, and tool loop |
| `model/dashscope/embedding` | DashScope text embedding model with batched inputs and dimension configuration |
| `model/dashscope/stt` | DashScope speech-to-text model reading a local WAV file and printing recognized text |
| `model/dashscope/tts` | DashScope text-to-speech model streaming audio chunks into `output.wav` |
| `model/deepseek/chat` | DeepSeek ChatModel non-streaming call, streaming call, and tool loop |
| `model/gemini/chat` | Gemini ChatModel multimodal message, token estimation, non-streaming call, streaming call, and tool loop |
| `model/moonshot/chat` | Moonshot ChatModel multimodal message, token estimation, non-streaming call, streaming call, and tool loop |
| `model/ollama/chat` | Local Ollama ChatModel non-streaming call, streaming call, and tool loop |
| `model/openai/chat` | OpenAI ChatModel non-streaming call, streaming call, proxy HTTP client, and tool loop |
| `model/xai/chat` | xAI ChatModel multimodal message, token estimation, non-streaming call, streaming call, and tool loop |
| `model/zhipu/chat` | Zhipu AI ChatModel non-streaming call, streaming call, token estimation, and tool loop |
| `agent/basic` | Agent + DashScope ChatModel + task tool end-to-end ReAct flow |
| `agent/team` | Process-local leader/worker Agent team tools and inbox delivery |
| `agent/configuration` | Agent model fallback, ReAct config, and local context cleanup |
| `agent/context_strategy` | Summary compression, workspace offload, and custom context strategies |
| `agent/external` | Agent pause/resume flow for tools executed outside the Go process |
| `agent/hooks` | Agent middleware hooks for reply, reasoning, model call, acting, and system prompt |
| `agent/middleware_tracing` | Tracing middleware for reply, model-call, and tool-execution spans |
| `agent/permission` | Agent permission confirmation and resume flow |
| `integration/gin` | Gin HTTP integration with direct ChatModel streaming and Agent event streaming |
| `integration/kratos` | Kratos HTTP integration with direct ChatModel streaming and Agent event streaming |
| `tool/function` | Custom function tool |
| `tool/builtin` | Built-in Bash/Edit/Glob/Grep/Read/Write tools |
| `tool/mcp` | MCP client integration, MCP tool wrapping, Toolkit execution, and DashScope ChatModel tool call |
| `tool/task` | TaskCreate/TaskGet/TaskList/TaskUpdate |
| `tool/skill` | Local `SKILL.md` loading |
| `workspace/local` | Workspace-backed tool file operations, skills, context offload, and tool result offload |
| `workspace/docker` | Docker workspace tools, container file operations, and DashScope ChatModel response |
| `workspace/microsandbox` | Microsandbox microVM workspace tools and DashScope ChatModel response |
| `workspace/daytona` | Daytona sandbox workspace running Python CSV data analysis |

## External Services

Model examples use provider-specific environment variables:

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

The OpenAI example also supports `AI_OPENAI_PROXY_URL` for local proxy access. The Ollama example uses `http://127.0.0.1:11434`; start Ollama and pull `llama3.1` before running it.

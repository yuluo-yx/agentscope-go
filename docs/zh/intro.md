# AgentScope Go

**AgentScope Go 是用于构建智能体式 LLM 应用的 Go 框架。**

AgentScope Go 提供智能体开发需要的基础组件：模型调用、消息协议、工具调用、运行状态、权限控制和本地 Workspace。它参考 Python AgentScope 的设计，同时采用更符合 Go 的显式包结构和类型化 API。

## 亮点

- **智能体循环**：`agent.Agent` 负责模型推理、工具执行、工具结果回传和最终回复。
- **消息协议**：`message.Message` 表达系统消息、用户消息、助手消息、工具调用、工具结果和数据块。
- **模型集成**：已提供 OpenAI、Anthropic、DashScope、DeepSeek、Moonshot、XAI 和 Ollama 聊天模型集成。
- **工具系统**：函数工具、内置文件和 Shell 工具、任务工具、Skill 加载器、MCP 工具适配器共用 `tool.Tool` 接口。
- **权限控制**：权限模式和规则决定工具调用是否允许执行。
- **运行状态**：`state.AgentState` 保存对话上下文、任务状态、权限上下文和工具缓存。
- **Workspace**：`workspace.LocalWorkspace` 为工具提供本地文件环境、Skill 资源和内容卸载能力。

## 环境要求

- Go 1.26.3 或更高版本。
- 运行真实模型示例时，需要对应模型服务的 API Key。
- DashScope 示例默认读取 `AI_DASHSCOPE_API_KEY`。

## 快速入口

1. 阅读[安装](quickstart/installation.md)。
2. 阅读[核心概念](quickstart/key-concepts.md)。
3. 按[构建智能体](quickstart/agent.md)创建最小智能体。
4. 查看[示例](task/examples.md)运行完整示例。

## 最小示例

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
)

func main() {
	model, err := dashscope.NewChatModel(
		dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
		"qwen3.7-max",
	)
	if err != nil {
		panic(err)
	}

	user, err := message.NewUserMessage("user", "Say hello in one short sentence.")
	if err != nil {
		panic(err)
	}

	response, err := model.Call(context.Background(), asmodel.CallRequest{
		Messages: []*message.Message{user},
	})
	if err != nil {
		panic(err)
	}

	for _, block := range response.Content {
		if text, ok := block.(*message.TextBlock); ok {
			fmt.Println(text.Text)
		}
	}
}
```

## 包结构

| 包 | 用途 |
| --- | --- |
| `agent` | ReAct 风格智能体循环和中间件钩子 |
| `message` | 消息和内容块协议 |
| `model` | 聊天模型接口和供应商实现 |
| `tool` | 工具接口、Toolkit、函数适配器和工具组 |
| `tool/builtin` | Bash、Edit、Glob、Grep、Read、Write 和 ResetTools |
| `tool/mcp` | MCP 客户端集成和 MCP 工具适配器 |
| `tool/task` | TaskCreate、TaskGet、TaskList 和 TaskUpdate |
| `tool/skill` | 本地 `SKILL.md` 加载器 |
| `permission` | 权限模式、规则、决策和执行引擎 |
| `state` | AgentState、ToolContext 和 TaskContext |
| `workspace` | 本地 Workspace 和内容卸载能力 |

## 社区

- GitHub：[yuluo-yx/agentscope-go](https://github.com/yuluo-yx/agentscope-go)

## 许可证

AgentScope Go 使用 Apache License 2.0。

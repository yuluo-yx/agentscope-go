# AgentScope-Go：翻译计划

## 目录

1. [背景与目标](#1-背景与目标)
2. [Python 源码概览](#2-python-源码概览)
3. [Go 包结构设计](#3-go-包结构设计)
4. [核心类型设计](#4-核心类型设计)
5. [Go 模块依赖清单](#5-go-模块依赖清单)
6. [关键架构决策](#6-关键架构决策)
7. [循环依赖与解决](#7-循环依赖与解决)
8. [翻译阶段](#8-翻译阶段)
9. [已知挑战与方案](#9-已知挑战与方案)
10. [测试策略](#10-测试策略)
11. [Python 到 Go 转换注意事项](#11-python-到-go-转换注意事项)
12. [测试框架落地计划](#12-测试框架落地计划)
13. [第三方模型 SDK 接入策略](#13-第三方模型-sdk-接入策略)
14. [证据来源与验收清单](#14-证据来源与验收清单)

---

## 1. 背景与目标

AgentScope 2.0 是阿里巴巴通义实验室的生产级多智能体框架，核心能力包括 ReAct 推理-执行循环、流式事件、工具调用（含 MCP）、权限系统、上下文压缩和中间件。

### 翻译原则

- **Go 原生设计**：不照搬 Python 的 API 分层，按 Go 惯例重新组织。
- **精简包数量**：紧密耦合的类型放同一个包，只为真正的独立关注点建子包。
- **渐进式交付**：按依赖关系分阶段，每阶段有明确验收标准。
- **SDK 优先**：第三方模型接入优先使用官方或事实标准 Go SDK，不自行维护底层 HTTP 协议实现。
- **依赖可审计**：引入依赖前记录用途、版本、替代方案和测试覆盖，避免把可选能力做成核心路径。
- **代码注释使用英文**：Go 源码中的包注释、导出标识符注释和必要实现注释统一使用英文；中文说明只保留在文档、计划和审查记录中。

---

## 2. Python 源码概览

Python 版本有 16 个顶层模块：`agent`、`message`、`event`、`model`、`tool`、`credential`、`formatter`、`state`、`permission`、`middleware`、`mcp`、`skill`、`workspace`、`embedding`、`app`、`_utils` + `exception` + `types`。

这个分层在 Python 中有意义（模块级隔离、可选依赖、延迟导入），但搬到 Go 中会导致大量小包和循环依赖。Go 的设计思路不同。

---

## 3. Go 包结构设计

### 设计思路

Python 的 16 个模块在 Go 中重组为 **根包 facade + 6 个领域包 + 4 个基础/辅助包 + 实现子包**：

- **根包**（`package agentscope`）：只保留常用 API 的兼容 facade，通过 type alias / var alias 重新导出 `agent/`、`state/`、`model/`、`tool/` 中的高频类型和构造器。
- **`agent/`**：Agent、Config、ReAct 推理-执行循环、中间件 hook 链 —— 框架的编排层。
- **`state/`**：AgentState、ToolContext、TaskContext、Task —— 可持久化运行状态。
- **`model/`**：ChatModel、CallRequest、ChatResponse、ChatUsage、ToolSchema，以及 Provider 共用适配能力。
- **`tool/`**：Tool、ToolChunk、ToolResponse、Toolkit、ToolGroup、FunctionTool、ResetTools。
- **`message/`**：Message、ContentBlock、Event 及相关类型 —— 因 message ↔ event 循环依赖必须同包。
- **`permission/`**：权限规则、引擎、决策 —— 被 message/、state/、tool/、agent/ 复用，独立成包避免倒置依赖。
- **`types/`**：JSON、Schema、ToolChoice 等跨模型、工具与 Agent 共用的轻量类型 —— 对齐 Python 顶层 `types` 模块，避免根包继续膨胀。
- **`errors/`**：AgentError、DeveloperError、ToolError 等框架错误 —— 对齐 Python 顶层 `exception` 模块，并保持 `errors.As` / `errors.Is` 可判定。
- **`utils/`**：跨包复用、无业务依赖的轻量工具函数，目前只放深拷贝辅助，避免根包或实现包重复维护 clone 逻辑。
- **`model/<provider>/`**：LLM Provider 实现（凭据和格式化器合入对应 Provider）。
- **`tool/builtin/`、`tool/mcp/`、`tool/skill/`、`tool/task/`**：工具实现子包。
- **`middleware/`**：可选中间件实现，例如 tracing；Middleware 契约在 `agent/`。
- **`internal/`**：JSON 修复、日志等内部工具。

**C 方案决策（2026-05-30）**：根包不再承载实现和协议契约，只做 facade。领域契约放回领域包：`model.ChatModel`、`tool.Tool`、`agent.Middleware`、`state.AgentState`。实现包只依赖对应领域包，不依赖根包，根包位于依赖图末端，避免继续膨胀。

其他 Python 模块的去向：

| Python 模块 | Go 去向 | 原因 |
|---|---|---|
| `agent` | `agent/`，根包 facade 重新导出 | 核心编排层独立，根包只保留兼容入口 |
| `message` | `message/` | 与 event 循环依赖，必须同包 |
| `event` | `message/` | 与 message 循环依赖，必须同包 |
| `state` | `state/`，根包 facade 重新导出 | 状态可被 tool、agent、workspace 复用，不应绑在根包 |
| `permission` | `permission/` | 被 message/ 导入，独立成包 |
| `credential` | `model/<provider>/` | 凭据只被对应 Provider 使用 |
| `formatter` | `model/<provider>/` | 格式化只被对应 Provider 使用 |
| `middleware` | 契约在 `agent/`，可选实现在 `middleware/` | 中间件拦截 Agent 生命周期，契约跟随 Agent |
| `mcp` | `tool/mcp/` | MCP 本质上是工具适配器 |
| `skill` | `tool/skill/` | 技能本质上是工具扩展 |
| `workspace` | `workspace/` | 执行环境隔离 |
| `embedding` | 后期按需 | 独立关注点，不阻塞核心 |
| `app` | 后期按需 | HTTP 服务层 |
| `_utils` | `internal/` + `utils/` | 默认进 `internal/`；跨包公开复用且无业务依赖的轻量能力才进 `utils/` |
| `exception` | `errors/` | Python 版是独立顶层模块；Go 版独立成包后，错误体系可以被模型、工具、Agent 共同复用 |
| `types` | `types/` | Python 版是独立顶层模块；Go 版只放跨包共享轻量类型，避免根包变成杂物入口 |

### 目录布局

本节是目标目录布局，不等于当前 `main` 已全部实现。当前实际包以 `go list ./...` 为准；截至 2026-05-30，`tool/skill/`、`workspace/` 基础 Local 实现、DeepSeek/DashScope/XAI/Moonshot 兼容 Provider 与 Ollama Provider 已落地；`embedding/`、`tool/mcp/`、Gemini Provider、Docker/E2B workspace、workspace MCP gateway 以及独立 TTS/Image/Video 生成模型接口仍未落地。

```
agentscope-go/
├── go.mod                              # module github.com/yuluo-yx/agentscope-go
├── go.sum
├── Makefile
├── README.md
│
│   ── 根包：兼容 facade ──
│
├── facade.go                           # 常用核心类型、构造器和 Option 的 type alias / var alias
│
├── agent/                              # Agent 编排层
│   ├── agent.go                        # Agent 结构体、Reply/ReplyStream
│   ├── reasoning.go                    # 推理逻辑（reasoning + model call）
│   ├── acting.go                       # 执行逻辑（tool call 调度 + 权限检查）
│   ├── context.go                      # 上下文压缩、分割
│   ├── config.go                       # ContextConfig、ReActConfig、ModelConfig
│   ├── middleware.go                   # Middleware 接口、AgentAccessor、Hook 类型
│   ├── middleware_chain.go             # Agent 内部中间件洋葱链适配
│   └── aliases.go                      # Agent 内部使用的 model/state/tool 类型别名
│
├── state/                              # 可持久化运行状态
│   ├── state.go                        # AgentState、ToolContext
│   └── task.go                         # Task、TaskContext
│
│   ── 核心子包：数据类型 ──
│
├── errors/                             # 框架错误体系（对齐 Python exception）
│   ├── error.go                        # AgentError、DeveloperError、错误 Option
│   └── tool.go                         # ToolNotFoundError、ToolInterruptedError 等
│
├── types/                              # 跨包共享轻量类型（对齐 Python types）
│   ├── types.go                        # JSONPrimitive、JSONSerializableObject、Embedding
│   ├── schema.go                       # JSONSchema
│   └── tool_choice.go                  # ToolChoice、ToolChoiceMode
│
├── utils/                              # 跨包轻量工具函数
│   └── clone.go                        # CloneAny、CloneAnyMap
│
├── message/                            # 消息 + 内容块 + 事件（因循环依赖合并）
│   ├── message.go                      # Message、Role、Usage、工厂函数
│   ├── block.go                        # ContentBlock 接口 + 6 种具体类型
│   ├── block_json.go                   # ContentBlock 自定义 JSON 编解码
│   ├── event.go                        # Event 接口 + EventType + 25 种事件
│   ├── event_json.go                   # Event 自定义 JSON 编解码
│   └── event_apply.go                  # ApplyEvent（事件 → Message 变更）
│
├── permission/                         # 权限系统（被 message/ 和根包引用）
│   ├── mode.go                         # PermissionMode、PermissionBehavior
│   ├── rule.go                         # PermissionRule
│   ├── context.go                      # PermissionContext
│   ├── engine.go                       # PermissionEngine
│   └── decision.go                     # PermissionDecision
│
│   ── 实现子包 ──
│
├── model/                              # LLM Provider 契约 + 实现
│   ├── core.go                         # ChatModel、CallRequest、ChatResponse、ChatUsage、ToolSchema
│   ├── adapter.go                      # SDK 响应归一化、错误归一化
│   ├── compat.go                       # OpenAI 兼容 Provider 共用配置
│   ├── anthropic/                      # Anthropic Provider
│   │   ├── model.go                    # 基于 anthropic-sdk-go 的 ChatModel 实现
│   │   ├── credential.go               # AnthropicCredential
│   │   ├── formatter.go               # 消息格式化
│   │   ├── params.go                   # 模型参数
│   │   └── models/                     # YAML 模型卡片
│   ├── openai/                         # OpenAI Provider
│   │   ├── chat_model.go               # 基于 openai-go 的 Chat Completions 适配
│   │   ├── response_model.go           # 基于 openai-go 的 Responses API 适配
│   │   ├── credential.go
│   │   ├── formatter.go
│   │   ├── params.go
│   │   └── models/
│   ├── deepseek/                       # DeepSeek（openai-go + BaseURL 兼容）
│   ├── dashscope/                      # DashScope（openai-go + OpenAI 兼容接口）
│   ├── gemini/                         # Gemini（Google Gen AI SDK）
│   ├── ollama/                         # Ollama（Ollama Go API）
│   ├── xai/                            # XAI（openai-go + BaseURL 兼容）
│   └── moonshot/                       # Moonshot/Kimi（openai-go + BaseURL 兼容）
│
├── tool/                               # 工具契约 + 实现
│   ├── core.go                         # Tool、ToolChunk、ToolResponse
│   ├── toolkit.go                      # Toolkit（注册、管理、执行）
│   ├── group.go                        # ToolGroup
│   ├── adapters.go                     # FunctionTool 适配器
│   ├── reset.go                        # ResetTools 元工具，避免 tool 包反向导入 builtin
│   ├── builtin/                        # 内置工具（package builtin，不使用 native 命名）
│   │   ├── bash.go
│   │   ├── common.go                   # 内置工具共享 schema、权限和 glob 辅助
│   │   ├── edit.go
│   │   ├── glob.go
│   │   ├── grep.go
│   │   ├── read.go
│   │   ├── write.go
│   │   └── meta.go                     # ResetTools 兼容 wrapper
│   ├── mcp/                            # MCP 工具适配
│   │   ├── client.go
│   │   ├── config.go
│   │   └── adapter.go
│   ├── task/                           # 任务管理工具
│   │   ├── task.go                     # 工具名常量 + NewTools 导出顺序
│   │   ├── base.go                     # 任务工具共享 Tool 元信息与默认权限
│   │   ├── create.go
│   │   ├── get.go
│   │   ├── helpers.go                  # 输入解析、单 chunk 输出、任务上下文辅助
│   │   ├── list.go
│   │   ├── update.go
│   │   └── descriptions.go             # 任务工具描述
│   └── skill/                          # 技能加载
│       ├── skill.go
│       └── loader.go
│
├── middleware/                          # 中间件实现（契约在 agent/middleware.go；可选）
│   └── tracing.go                      # OpenTelemetry 追踪（后续按可选依赖接入）
│
├── workspace/                           # 执行工作空间（基础 Local 已落地；Docker/E2B 后续）
│   ├── workspace.go
│   ├── local.go
│   ├── docker.go
│   ├── e2b.go
│   └── offloader.go
│
├── internal/                            # 内部工具（不导出）
│   ├── jsonutil/
│   │   ├── repair.go
│   │   └── repair_test.go
│   ├── logging/
│   │   └── logging.go
│   └── testutil/                       # 单元测试辅助：mock 模型、事件收集器、cmp 选项
│       ├── model.go
│       ├── event.go
│       └── cmp.go
│
├── test/                                # 跨包集成测试，参考 Kueue 的 test/ 分层
│   ├── util/                            # 集成测试常量、fixture、mock server
│   │   ├── constants.go
│   │   ├── fixtures.go
│   │   └── mockserver.go
│   ├── package_structure/               # 根包 facade 与领域包类型兼容性验证
│   │   └── package_structure_test.go
│   └── integration/
│       ├── agent/
│       │   ├── suite_test.go            # Ginkgo/Gomega 套件入口
│       │   └── react_test.go
│       ├── model/
│       │   ├── suite_test.go
│       │   └── provider_contract_test.go
│       └── tool/
│           ├── suite_test.go
│           └── toolkit_test.go
│
└── example/                             # 使用示例
    └── main.go
```

### 用户视角的 API

```go
import (
    "context"
    "fmt"
    "os"

    "github.com/yuluo-yx/agentscope-go/agent"
    "github.com/yuluo-yx/agentscope-go/message"
    "github.com/yuluo-yx/agentscope-go/model/openai"
    "github.com/yuluo-yx/agentscope-go/tool"
    "github.com/yuluo-yx/agentscope-go/tool/builtin"
)

func main() {
    ctx := context.Background()
    model, err := openai.NewChatModel(openai.NewCredential(os.Getenv("OPENAI_API_KEY")), "gpt-4o")
    if err != nil {
        panic(err)
    }
    kit, err := tool.NewToolkit(
        builtin.NewBash(),
        builtin.NewRead(),
        builtin.NewWrite(),
    )
    if err != nil {
        panic(err)
    }
    assistantAgent, err := agent.NewAgent(
        "Friday",
        "You are a helpful assistant.",
        model,
        agent.WithToolkit(kit),
    )
    if err != nil {
        panic(err)
    }

    userMessage, err := message.NewUserMessage("Tony", "Hi, Friday!")
    if err != nil {
        panic(err)
    }

    err = assistantAgent.ReplyStream(ctx, userMessage, func(evt message.Event) error {
        switch e := evt.(type) {
        case *message.TextBlockDeltaEvent:
            fmt.Print(e.Delta)
        }
        return nil
    })
}
```

根包 `agentscope` 保留兼容 facade。既可以使用上面的领域包导入，也可以继续通过 `agentscope.NewAgent`、`agentscope.NewAgentState`、`agentscope.NewChatResponse` 等 alias 使用高频 API。

---

## 4. 核心类型设计

### 4.0 共享类型与错误包（types/、errors/）

Python 版将 `types` 和 `exception` 做成顶层模块。Go 版也保留独立包边界，但只把真正跨包共用的类型放进去：

```go
// types/types.go
package types

type JSONPrimitive = any
type JSONSerializableObject = any
type Embedding []float64

// types/schema.go
type JSONSchema map[string]any

// types/tool_choice.go
type ToolChoiceMode string
type ToolChoice struct {
    Mode  string   `json:"mode"`
    Tools []string `json:"tools,omitempty"`
}
```

```go
// errors/error.go
package errors

type AgentError struct {
    Message string
    Cause   error
}

type DeveloperError struct {
    Message string
    Cause   error
}

// errors/tool.go
type ToolNotFoundError struct { /* ... */ }
type ToolInterruptedError struct { /* ... */ }
type ToolJSONDecodeError struct { /* ... */ }
type ToolGroupInactiveError struct { /* ... */ }
```

命名约束：

- `types/` 不做功能聚合包，只放 JSON、Schema、Embedding、ToolChoice 这类跨模型与工具边界共用的轻量类型。
- `errors/` 的导入路径与标准库 `errors` 同名，调用方需要同时使用标准库时应显式加别名，例如 `agenterrors "github.com/yuluo-yx/agentscope-go/errors"`。
- 根包不重新导出全部错误和共享类型；只有高频构造器可以按实际 API 体验再评估是否提供短路径。

### 4.1 内容块（message/block.go）

```go
package message

import "github.com/yuluo-yx/agentscope-go/permission"

// ContentBlock 是所有内容块类型实现的接口。
type ContentBlock interface {
    BlockType() string
    BlockID() string
    Clone() ContentBlock
    contentBlock()           // 未导出标记方法
}

type TextBlock struct {
    Type string `json:"type"`
    Text string `json:"text"`
    ID   string `json:"id"`
}

type ThinkingBlock struct {
    Type     string         `json:"type"`
    Thinking string         `json:"thinking"`
    ID       string         `json:"id"`
    Extra    map[string]any `json:"-"`  // Provider 特有字段
}

type HintBlock struct {
    Type string `json:"type"`
    Hint string `json:"hint"`
    ID   string `json:"id"`
}

type ToolCallState string
const (
    ToolCallPending   ToolCallState = "pending"
    ToolCallAsking    ToolCallState = "asking"
    ToolCallAllowed   ToolCallState = "allowed"
    ToolCallSubmitted ToolCallState = "submitted"
    ToolCallFinished  ToolCallState = "finished"
)

type ToolCallBlock struct {
    Type           string              `json:"type"`
    ID             string              `json:"id"`
    Name           string              `json:"name"`
    Input          string              `json:"input"`
    State          ToolCallState       `json:"state"`
    SuggestedRules []permission.Rule   `json:"suggested_rules,omitempty"`
    Extra          map[string]any      `json:"-"`
}

type ToolResultState string
const (
    ToolResultSuccess     ToolResultState = "success"
    ToolResultError       ToolResultState = "error"
    ToolResultInterrupted ToolResultState = "interrupted"
    ToolResultDenied      ToolResultState = "denied"
    ToolResultRunning     ToolResultState = "running"
)

type ToolResultBlock struct {
    Type   string           `json:"type"`
    ID     string           `json:"id"`
    Name   string           `json:"name"`
    Output ToolResultOutput `json:"output"`
    State  ToolResultState  `json:"state"`
}

// ToolResultOutput 处理 Python 的 Union[str, List[TextBlock | DataBlock]]。
type ToolResultOutput struct {
    Raw    string         // 纯字符串输出
    Blocks []ContentBlock // 块列表输出（仅 TextBlock | DataBlock）
}

type DataBlock struct {
    Type   string     `json:"type"`
    ID     string     `json:"id"`
    Source DataSource `json:"source"`
    Name   *string    `json:"name,omitempty"`
}

type DataSource interface {
    SourceType() string
    dataSource()
}

type Base64Source struct {
    Type      string `json:"type"`
    Data      string `json:"data"`
    MediaType string `json:"media_type"`
}

type URLSource struct {
    Type      string `json:"type"`
    URL       string `json:"url"`
    MediaType string `json:"media_type"`
}
```

### 4.2 消息（message/message.go）

```go
package message

type Role string
const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleSystem    Role = "system"
)

type Usage struct {
    InputTokens  int `json:"input_tokens"`
    OutputTokens int `json:"output_tokens"`
}

type Message struct {
    Name       string           `json:"name"`
    Content    ContentBlockList `json:"content"`
    Role       Role             `json:"role"`
    ID         string           `json:"id"`
    Metadata   map[string]any   `json:"metadata,omitempty"`
    CreatedAt  string           `json:"created_at"`
    FinishedAt *string          `json:"finished_at,omitempty"`
    Usage      *Usage           `json:"usage,omitempty"`
}

// 工厂函数
type MessageOption func(*Message)
func WithMessageID(id string) MessageOption
func WithMessageMetadata(md map[string]any) MessageOption

func NewUserMessage(name string, content any, opts ...MessageOption) (*Message, error)
func NewAssistantMessage(name string, content any, opts ...MessageOption) (*Message, error)
func NewSystemMessage(name string, content any, opts ...MessageOption) (*Message, error)
func MustAssistantMessage(name string, content any, opts ...MessageOption) *Message
func MustSystemMessage(name string, content any, opts ...MessageOption) *Message

// 语义约定：
// - New*Message 负责承接 Python 版 Pydantic 校验失败路径，统一返回 error。
// - Must*Message 只用于测试、示例或内部确定合法的硬编码消息；失败时 panic。
// - AssistantMessage 可能来自模型 Provider、事件回放和存储反序列化，不把适配错误升级为进程级 panic。
// - SystemMessage 通常来自初始化配置或中间件，校验失败应由初始化调用方处理。

// 方法
func (m *Message) HasContentBlocks(types ...string) bool
func (m *Message) GetTextContent(separator string) *string
func (m *Message) GetContentBlocks(types ...string) []ContentBlock
func (m *Message) FindBlock(blockType, blockID string) ContentBlock
func (m *Message) Clone() *Message
```

### 4.3 事件系统（message/event.go）

```go
package message

type EventType string
const (
    ReplyStartType           EventType = "REPLY_START"
    ReplyEndType             EventType = "REPLY_END"
    ModelCallStartType       EventType = "MODEL_CALL_START"
    ModelCallEndType         EventType = "MODEL_CALL_END"
    TextBlockStartType       EventType = "TEXT_BLOCK_START"
    TextBlockDeltaType       EventType = "TEXT_BLOCK_DELTA"
    TextBlockEndType         EventType = "TEXT_BLOCK_END"
    // ... 其余 20+ 种事件类型
)

type Event interface {
    GetType() EventType
    GetID() string
    GetTime() string
    ReplyID() string
    event()
}

type EventBase struct {
    ID        string `json:"id"`
    CreatedAt string `json:"created_at"`
}
```

### 4.4 模型接口（model/core.go）

```go
package model

import "github.com/yuluo-yx/agentscope-go/message"

type ChatModel interface {
    Name() string
    Call(context.Context, CallRequest) (*ChatResponse, error)
    Stream(context.Context, CallRequest) (<-chan ChatResponse, error)
    CountTokens(CallRequest) (int, error)
}
```

结构化输出不放入阶段四的 `ChatModel` 最小接口；后续 Provider 能力稳定后，再以独立接口或可选方法补齐，避免阶段四把所有 Provider 都绑定到未完成能力上。

### 4.5 工具接口（tool/core.go）

```go
package tool

import (
    "github.com/yuluo-yx/agentscope-go/permission"
    "github.com/yuluo-yx/agentscope-go/state"
)

type Tool interface {
    Name() string
    Description() string
    InputSchema() map[string]any
    IsConcurrencySafe() bool
    IsReadOnly() bool
    IsExternalTool() bool
    IsStateInjected() bool
    IsMCP() bool
    MCPName() string

    CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error)
    MatchRule(string, map[string]any) bool
    GenerateSuggestions(map[string]any) []permission.Rule
    Execute(context.Context, map[string]any, *state.AgentState) (<-chan ToolChunk, error)
}
```

### 4.6 中间件接口（agent/middleware.go）

```go
package agent

// AgentAccessor 是中间件可访问的智能体上下文（打破循环依赖）。
type AgentAccessor interface {
    AgentName() string
    AgentState() *AgentState
}

type Middleware interface {
    MiddlewareName() string
}

type ReplyMiddleware interface {
    OnReply(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)
}

type ReasoningMiddleware interface {
    OnReasoning(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)
}

type ActingMiddleware interface {
    OnActing(context.Context, AgentAccessor, HookInput, ToolHandler) (<-chan ToolChunk, error)
}

type ModelCallMiddleware interface {
    OnModelCall(context.Context, AgentAccessor, HookInput, ModelCallHandler) (*ChatResponse, error)
}

type SystemPromptMiddleware interface {
    OnSystemPrompt(context.Context, AgentAccessor, string) (string, error)
}
```

---

## 5. Go 模块依赖清单

### 核心依赖

| 用途 | Go 库 | 说明 |
|---|---|---|
| HTTP 测试服务 | `net/http`、`net/http/httptest`（标准库） | 仅用于测试 mock server 和内部服务，不用于手写模型 Provider 协议 |
| JSON Schema | `github.com/santhosh-tekuri/jsonschema/v6` | 工具输入验证 |
| UUID | `github.com/google/uuid` | 消息/事件 ID 生成 |
| 日志 | `log/slog`（标准库） | 结构化日志 |
| 模板 | `text/template`（标准库） | 技能模板 |
| YAML | `gopkg.in/yaml.v3` | 模型卡片 |
| Shell 解析 | `mvdan.cc/sh/v3` `v3.13.1` | Bash 工具权限检查使用 AST 解析，覆盖引号、管道、复合命令、重定向和 env assignment，替代手写字符串切分 |
| 结构验证 | `github.com/go-playground/validator/v10` | 配置验证 |
| 并发编排 | `golang.org/x/sync/errgroup` | 并发工具执行、错误收集、上下文取消 |
| 差异比较 | `github.com/google/go-cmp/cmp` | 单元测试结构体比较 |
| BDD 集成测试 | `github.com/onsi/ginkgo/v2`、`github.com/onsi/gomega` | 参考 Kueue 的 suite 组织方式 |
| Mock 生成 | `go.uber.org/mock` | 接口 mock，主要用于 ChatModel、Tool、Middleware |
| 分布式追踪 | `go.opentelemetry.io/otel` | 追踪中间件；默认可选，不进入核心路径 |
| MCP | `github.com/mark3labs/mcp-go` | MCP 协议客户端 |
| Redis | `github.com/redis/go-redis/v9` | app 存储（可选） |

### Provider 策略

所有 LLM Provider 优先使用官方 SDK 或可审计的事实标准 SDK。禁止在 Provider 中自行拼接 HTTP 请求、手写 SSE 协议解析或复制 SDK 已覆盖的鉴权、重试和错误模型。共享基础设施放在 `model/adapter.go` 和 `model/compat.go`，只负责把 SDK 类型归一化为框架接口（ChatModel、CallRequest、ChatResponse、ChatUsage），不下沉为通用 HTTP 客户端。

| Provider | SDK / 模块 | 接入方式 | 说明 |
|---|---|---|---|
| OpenAI | `github.com/openai/openai-go` | 官方 SDK | 同时适配 Chat Completions 与 Responses API |
| Anthropic | `github.com/anthropics/anthropic-sdk-go` | 官方 SDK | 保留 thinking blocks、tool use、流式事件语义 |
| Gemini | `google.golang.org/genai` | Google Gen AI SDK | 适配多模态 block 与结构化输出 |
| DashScope | `github.com/openai/openai-go` | OpenAI 兼容 BaseURL | 使用 DashScope OpenAI 兼容接口，不引入阿里云专用 SDK |
| Ollama | `github.com/ollama/ollama/api` | Ollama Go API | 本地模型场景，避免手写 REST 适配；当前项目已升级到 Go 1.26.3，并接入 `github.com/ollama/ollama` `v0.24.0` stable latest |
| DeepSeek | `github.com/openai/openai-go` | OpenAI 兼容 BaseURL | 复用 OpenAI SDK 的请求和流式能力 |
| XAI | `github.com/openai/openai-go` | OpenAI 兼容 BaseURL | 单独封装模型卡片、默认参数和错误映射 |
| Moonshot / Kimi | `github.com/openai/openai-go` | OpenAI 兼容 BaseURL | 单独封装模型卡片、默认参数和错误映射 |

SDK 接入边界：

- `model/<provider>` 只暴露框架的 `ChatModel` 实现、凭据配置和参数结构，不把 SDK 原始类型泄漏给根包。
- SDK 错误统一转换为 `ModelError`、`RateLimitError`、`AuthenticationError`、`ContextLengthError` 等框架错误类型，并保留可审计的原始错误文本。
- OpenAI 兼容 Provider 可以共享 `model/compat.go` 的 BaseURL、Header、模型卡片和参数校验逻辑，但必须在独立子包中声明供应商名称、默认模型和能力矩阵。
- 流式事件不暴露 SDK chunk，统一转成 `message.Event` 或 `ChatResponse`。如果 SDK 暂不支持某能力，文档中标记为“不支持”，不退回手写 HTTP。

---

## 6. 关键架构决策

### 6.1 异步生成器 → Yield 回调

```go
// 流式输出（agent/agent.go；根包 facade 重新导出）
func (a *Agent) ReplyStream(ctx context.Context, inputs Input,
    yield func(message.Event) error) error

// 便捷方法：消费全部事件，返回最终消息
func (a *Agent) Reply(ctx context.Context, inputs Input) (*message.Message, error)
```

选回调而非 channel 的原因：无 goroutine 泄漏、自然的错误传播和取消、与 Go 1.23 range-over-func 一致。

并发工具执行仍用 `errgroup.Group` + 缓冲 channel 做 fan-in。

### 6.2 判别联合 → 接口 + Type Switch + 两步 JSON 解码

```go
// message/block_json.go
func UnmarshalContentBlock(data []byte) (ContentBlock, error) {
    var probe struct{ Type string `json:"type"` }
    json.Unmarshal(data, &probe)
    switch probe.Type {
    case "text":   var b TextBlock; return &b, json.Unmarshal(data, &b)
    case "thinking": var b ThinkingBlock; ...
    }
}
```

### 6.3 context.Context 贯穿所有 I/O

```go
func (a *Agent) ReplyStream(ctx context.Context, ...) error
func (m ChatModel).Call(ctx context.Context, ...) error
func (t Tool).Execute(ctx context.Context, ...) error
```

### 6.4 中间件洋葱模型

```go
// agent/middleware_chain.go
func (a *Agent) applyReplyHooks(ctx context.Context, input HookInput,
    final EventHandler) (<-chan message.Event, error)
```

阶段四落地采用“双层流式模型”：Agent 对用户公开 `ReplyStream(ctx, input, yield)`，保证调用方的错误传播和取消语义；中间件内部继续使用 channel 作为 hook 的组合边界，便于 Reply、Reasoning、Acting 三类异步事件流复用同一套洋葱链。无 hook 的常规路径同步执行，不通过 goroutine 包装，避免模型错误被吞掉。

### 6.5 根包 facade，领域包承载契约

ChatModel、Tool、Middleware 不再定义在根包。`model.ChatModel`、`tool.Tool`、`agent.Middleware` 分别位于领域包；根包 `agentscope` 只通过 alias 重新导出高频 API。实现包依赖领域包，不依赖根包 facade，根包处于依赖图末端，避免未来 Provider、工具、workspace 继续把根包撑大。

---

## 7. 循环依赖与解决

### 依赖关系分析（来自 Python 源码实际 import）

```
permission/  → (无导出依赖)
types/       → (无导出依赖)
errors/      → (仅依赖标准库)
utils/       → (无导出依赖)
message/     → permission/  (PermissionRule, 直接导入)
             → event 类型   (Python 用 local import hack 打破循环)
event/       → message/     (ToolCallBlock, ToolResultBlock, ToolResultState)
             → permission/  (PermissionRule)
state/       → message/     (TextBlock, DataBlock, Message)
             → permission/  (PermissionContext)
tool/        → message/ + permission/ + state/
model/       → message/ + tool/
agent/       → 全部
```

### Go 解决方案

**关键发现**：message ↔ event 循环依赖 → 合并为 `message/` 包。permission 被 message/ 引用但不引用 message/ → 独立成包。Python 的 `types` 与 `exception` 都是顶层模块，Go 版也保留 `types/` 与 `errors/` 包边界，但要求它们不反向导入根包或实现包。`utils/` 只承载无业务依赖的跨包辅助函数，当前用于统一深拷贝语义。

**领域包承载契约**：`state/` 被 `tool/` 和 `agent/` 复用；`model/` 承载模型请求、响应和工具 schema；`tool/` 承载工具接口、工具结果和 Toolkit；`agent/` 只编排 model/tool/state/message/permission。根包 `agentscope` 只导入领域包做 facade，不被任何实现包导入。

```
types/        （无依赖）
errors/       （仅标准库）
utils/        （无依赖）
permission/   （无依赖）
    ↓
message/      （导入 permission/）
    ↓
state/        （导入 message/、permission/、utils/）
    ↓
model/        （导入 message/、types/、utils/）
tool/         （导入 state/、model/、message/、permission/、types/、utils/）
agent/        （导入 state/、model/、tool/、message/、permission/）
model/*       （导入 model/ + message/ + types/ + errors/）
tool/builtin/ （导入 tool/ + state/ + message/ + permission/）
middleware/   （导入 agent/）
workspace/    （导入 agent/、state/ 或 tool/，按具体实现最小依赖）
    ↓
根包 facade   （导入 agent/、state/、model/、tool/）
example/      （可导入根包 facade，也可直接导入领域包）
```

**所有箭头单向，无循环。**

---

## 8. 翻译阶段

### 阶段一：基础类型与子包（第 1-2 周）

实现 `permission/`、`message/` 两个核心子包和 `internal/` 工具。

| 步骤 | 文件 | 验收标准 |
|---|---|---|
| 1.1 | `internal/logging/logging.go` | slog 封装可用 |
| 1.2 | `internal/jsonutil/repair.go` + test | JSON 修复解析可用 |
| 1.3 | `permission/mode.go`、`rule.go`、`decision.go` | 权限模式、规则、决策类型 |
| 1.4 | `permission/context.go`、`engine.go` | 完整权限引擎 |
| 1.5 | `message/block.go`、`block_json.go` | 6 种内容块 + JSON 编解码 |
| 1.6 | `message/message.go` | Message + 工厂函数 + Clone() |
| 1.7 | `message/event.go`、`event_json.go` | 25 种事件 + JSON 编解码 |
| 1.8 | `message/event_apply.go` | ApplyEvent（事件 → Message 变更） |

**里程碑**：`go test ./permission/... ./message/... ./internal/...` 全部通过。

### 阶段二：基础包迁移 + 领域契约 + 模型（第 2-3 周）

先把错误体系和跨包共享类型迁移到独立包，再在 `model/`、`tool/`、`state/`、`agent/` 中定义领域契约和核心类型，实现模型 Provider。根包只在最后以 alias facade 形式提供兼容入口。

| 步骤 | 文件 | 验收标准 |
|---|---|---|
| 2.1 | `errors/error.go`、`errors/tool.go`、`types/types.go`、`types/schema.go`、`types/tool_choice.go`、`utils/clone.go` | 错误体系、共享类型和跨包深拷贝辅助独立成包，根包不再承载 `errors.go`、`types.go` 或 clone helper |
| 2.2 | `model/core.go` | ChatModel 接口、CallRequest、ChatResponse、ChatUsage |
| 2.3 | `tool/core.go` | Tool 接口、ToolChunk、ToolResponse |
| 2.4 | `agent/middleware.go` | Middleware 接口、AgentAccessor、Hook 类型 |
| 2.5 | `state/state.go`、`state/task.go` | AgentState + ToolContext + TaskContext |
| 2.6 | `agent/config.go` | ContextConfig、ReActConfig、ModelConfig |
| 2.7 | `model/adapter.go`、`model/compat.go` | SDK 响应归一化 + OpenAI 兼容 Provider 基础配置 |
| 2.8 | `model/openai/` | 基于 openai-go 的 ChatModel 实现 + SDK mock 测试 |
| 2.9 | `model/anthropic/` | 基于 anthropic-sdk-go 的 ChatModel 实现 + SDK mock 测试 |

**里程碑**：`go test ./...` 通过，mock SDK transport 或 `httptest.NewServer` 完成 OpenAI/Anthropic 流式调用测试。测试只模拟服务端行为，不在生产代码中复制 HTTP 协议实现。

**执行记录（2026-05-28）**：已先落地 2.1～2.7 的公共契约层，包括错误体系、共享 JSON 类型、`ChatModel`/`Tool`/`Middleware` 接口、状态与任务上下文、配置默认值，以及 `model/` 子包的 SDK 无关响应归一化和 OpenAI 兼容 Provider 基础配置。

**执行记录（2026-05-29）**：根据 Python 版 `agentscope/types` 与 `agentscope/exception` 的顶层包结构，已将根包 `errors.go`、`types.go` 迁移为 `errors/` 与 `types/` 独立包，并同步更新领域接口、Provider、工具系统和测试中的导入路径，避免继续扩大根包 API 面。

**计划修订（2026-05-29）**：深拷贝辅助函数统一放入 `utils/clone.go`，`model/`、`tool/`、`state/` 等领域包复用 `utils.CloneAnyMap` / `utils.CloneAny`，避免浅拷贝与深拷贝逻辑分散。

**计划修订（2026-05-30）**：C 方案取代早期“根包承载核心契约”的临时设计。`model/core.go`、`tool/core.go`、`state/` 和 `agent/` 承载领域契约，根包 `facade.go` 只做 alias 重新导出；根目录不放领域单测，facade 兼容性测试放入 `test/package_structure/`。

**执行记录（2026-05-29）**：已落地 2.8 的 `model/openai` Chat Completions Provider，使用 `github.com/openai/openai-go` 官方 SDK。当前实现覆盖非流式调用、流式增量合并、工具 schema 过滤、强制工具选择、Assistant tool call 历史、tool result 历史、用量归一化、错误归一化和近似 token 计数。

**执行记录（2026-05-29）**：已落地 2.9 的 `model/anthropic` Messages API Provider，使用 `github.com/anthropics/anthropic-sdk-go` 官方 SDK。当前实现覆盖非流式调用、流式事件合并、top-level system prompt、tool use、tool result、工具 schema 过滤、强制工具选择、thinking 响应保留、用量归一化、错误归一化和近似 token 计数。thinking 默认关闭，仅在显式配置 `ThinkingBudgetTokens` 时开启。

### 阶段三：工具系统（第 3-4 周）

内置工具统一放在 `tool/builtin/`，Go 包名为 `builtin`。不使用 `tool/native/` 或 `tools/native/`：`native` 容易与 Provider 原生 SDK、原生能力适配等概念冲突，而 Python 版实际目录是 `agentscope/tool/_builtin/`，Go 版应沿用这个语义。

| 步骤 | 文件 | 验收标准 |
|---|---|---|
| 3.1 | `tool/toolkit.go`、`tool/group.go` | Toolkit 注册、Schema 生成、执行 |
| 3.2 | `tool/adapters.go` | FunctionTool 适配器 |
| 3.3 | `tool/builtin/` | Bash、Read、Write、Edit、Glob、Grep、ResetTools；目录与包名都使用 `builtin` |

**里程碑**：`go test ./tool/...` 全部通过。

**执行记录（2026-05-29）**：已落地 `tool` 包的 Toolkit、ToolGroup 与 FunctionTool 适配器。当前 API 包括 `NewToolkit`、`NewToolkitWithGroups`、`ToolSchemas`、`AvailableTools`、`CallTool` 和 `RunTool`；basic 工具始终可见，命名 ToolGroup 通过 `ToolContext.ActivatedGroups` 激活，有可选分组时自动暴露 `reset_tools` 元工具。

**执行记录（2026-05-29）**：已落地 `tool/builtin` 包，包含 Bash、Read、Write、Edit、Glob、Grep 和 ResetTools 兼容入口。文件工具遵循 Python 版的 `file_path`、绝对路径、Read 缓存、Write/Edit 先读后改、危险路径复核和 `accept_edits` 工作目录自动允许语义；Read 输出使用 6 位行号加制表符格式；Glob/Grep 为只读工具并复用 Go 侧 glob/regexp 实现。

**计划修订（2026-05-30）**：为避免 `tool` 父包反向导入 `tool/builtin` 形成循环，`ResetTools` 的实现归入 `tool/reset.go`，`tool/builtin/meta.go` 仅保留兼容 wrapper；内置工具实现仍位于 `tool/builtin/` 且包名为 `builtin`。

**阶段三差异记录（2026-05-30）**：Python Bash 工具使用 tree-sitter 做 Bash AST 级解析；Go 版不引入 tree-sitter 绑定，改用纯 Go 的 `mvdan.cc/sh/v3` `v3.13.1` 解析 Bash AST。当前实现已覆盖引号内 shell 操作符、env assignment、复合命令、输出重定向、动态 shell 结构、危险命令模式、sed 安全约束、敏感路径复核和 `accept_edits` 文件系统命令路径；普通非只读命令返回 `PASSTHROUGH`，由权限规则和默认模式继续裁决，避免阻断用户显式 allow rule。

**阶段三沙箱核查记录（2026-05-30）**：Python 版没有给 `LocalWorkspace` 内置工具额外套一层通用工具沙箱。`LocalWorkspace.list_tools()` 直接返回 `Bash`、`Edit`、`Glob`、`Grep`、`Read`、`Write`；`Bash.__call__` 通过 `asyncio.create_subprocess_shell` 在本机执行，主要保护来自权限系统、静态 Bash 检查、超时和输出截断。Python 的隔离能力存在于 workspace 后端：`DockerWorkspace` 与 `E2BWorkspace` 的系统提示明确要求工具调用在容器或 E2B sandbox 内执行，且 `list_tools()` 返回空列表，工具通过 in-container / in-sandbox MCP gateway 暴露。Go 阶段三因此不实现本地工具沙箱；后续若翻译 `workspace` 包，再按 Docker/E2B workspace 级隔离设计补齐。

### 阶段四：Agent 核心（第 4-5 周）

| 步骤 | 文件 | 验收标准 |
|---|---|---|
| 4.1 | `agent/middleware.go`、`agent/middleware_chain.go` | Agent hook 类型 + 链式执行；OpenTelemetry 追踪中间件作为可选实现包后续接入，不进入阶段四核心路径 |
| 4.2 | `agent/reasoning.go` | 推理逻辑 + 模型调用 |
| 4.3 | `agent/acting.go` | 工具调用执行 + 权限检查 + 并发批处理 |
| 4.4 | `agent/agent.go` | 完整 ReAct 循环 |
| 4.5 | `agent/context.go` | 上下文压缩 |

**里程碑**：基于 mock 模型的完整 ReAct 集成测试通过。

**执行记录（2026-05-30）**：已落地阶段四 Agent 核心闭环。`agent/` 新增 `Agent`、`NewAgent`、`Reply`、`ReplyStream` 与 `ToolProvider` 最小接口；`tool.Toolkit` 通过 `FindTool`、`ToolSchemas`、`CallTool` 满足 Agent 依赖。根包只在 `facade.go` 中 alias 这些高频 API，避免实现包依赖根包。

**执行记录（2026-05-30）**：已落地 `agent/reasoning.go`、`agent/acting.go`、`agent/middleware_chain.go`、`agent/context.go`。当前覆盖纯文本回复、模型调用事件、工具调用后继续推理、权限确认暂停、权限 `UpdatedInput` 写回工具输入、模型错误传播、系统提示词 hook、模型调用 hook 修改 request、concurrency-safe 本地工具并发批处理，以及工具结果截断式上下文整理。

**阶段四边界记录（2026-05-30）**：Python 版 tracing middleware 依赖 OpenTelemetry SDK，且本计划第 5 节已标注追踪为可选能力、不进入核心路径。Go 阶段四只实现中间件 hook 链，暂不引入 `go.opentelemetry.io/otel`；后续若实现 `middleware/` 包，再把 tracing 作为独立可选实现包接入。

**阶段四上下文差异记录（2026-05-30）**：Go 版尚未翻译 `workspace/` 与 Offloader，因此 `CompressContext` 当前只做本地可完成的工具结果长度截断。跨窗口摘要压缩、外部存储和 workspace 级上下文卸载放入阶段五 `workspace/` 与 Offloader。

### 阶段五：扩展（第 5-6 周）

| 步骤 | 内容 | 说明 |
|---|---|---|
| 5.1 | `tool/task/` | 任务管理工具，对齐 Python `TaskCreate`、`TaskList`、`TaskGet`、`TaskUpdate` 的状态注入、默认允许和任务依赖语义 |
| 5.2 | `test/e2e/global/` | 全局 E2E 冒烟测试，默认使用本地 fake model + 本地工具 + Agent 状态闭环，不依赖外部 Provider 或 API Key |
| 5.3 | `tool/mcp/` | MCP 客户端 + 工具适配 |
| 5.4 | `tool/skill/` | 技能加载 |
| 5.5 | `model/deepseek/`、`model/ollama/` 等 | 剩余 Provider |
| 5.6 | `workspace/` | 工作空间 + Offloader |
| 5.7 | `example/` | 独立 Go module 示例；每个子目录包含 `go.mod`、`main.go`、英文 `README.md` 和中文 `README-zh.md`，默认本地可验证，真实 Provider 调用使用环境变量显式开启 |

**阶段五计划调整记录（2026-05-30）**：用户最初明确要求阶段五暂不处理 `example/`，并补充全局 E2E 测试。Go 版阶段五因此先落地不依赖外部服务的 `tool/task/` 与 `test/e2e/global/`，再推进 MCP、skill、剩余 Provider 和 workspace。后续用户要求恢复 `example/`，并要求每个示例子目录都是独立小项目，包含各自 `go.mod`、英文 `README.md` 与中文 `README-zh.md`。Provider 真实 E2E 仍保留环境变量控制，避免把外部模型稳定性纳入默认本地验证门槛。

**阶段五执行记录（2026-05-30）**：已落地 `tool/task/`，提供 `TaskCreate`、`TaskList`、`TaskGet`、`TaskUpdate` 和 `NewTools()`。实现对齐 Python 版状态注入、默认允许、任务创建、列表摘要、详情读取、状态更新、删除、owner、metadata merge、`add_blocks` 与 `add_blocked_by` 双向依赖关系。Go 版存在一个有意差异：Python task 基类将所有 task 工具标记为 concurrency safe；Go 版为了避免多个工具同时写同一 `AgentState.TaskContext` 造成数据竞争，仅将只读的 `TaskList`、`TaskGet` 标记为并发安全，`TaskCreate`、`TaskUpdate` 顺序执行。

**阶段五 E2E 记录（2026-05-30）**：已新增 `test/e2e/global/` 本地全局冒烟测试，覆盖 Agent 调用 fake model、暴露工具 schema、执行 `TaskCreate`、写入 `AgentState.TaskContext`、把工具结果回填到下一次模型调用并生成最终回复的完整闭环。该测试不依赖外部 Provider 或环境变量，纳入默认 `go test ./...`。

**阶段五出入核查记录（2026-05-30）**：已整理 `tool/task/` 文件布局，使实际代码与本计划中的 `create.go`、`get.go`、`list.go`、`update.go` 拆分保持一致，并补充 `base.go`、`helpers.go`、`descriptions.go` 承载共享逻辑。已修正 `state.NewTask` 的 ID 生成方式：Python 版使用 `uuid.uuid4().hex`，Go 版同步为 32 位小写 hex UUID，避免带连字符 UUID 与 Python 持久化数据产生格式出入。`NewTools()` 的顺序同步 Python `_task/__init__.py` 的导出顺序：`TaskCreate`、`TaskGet`、`TaskList`、`TaskUpdate`。

**阶段五执行记录（2026-05-30）**：已落地 `tool/skill/`，提供 `Skill`、`Loader`、`LocalLoader`、`LoadDir`、`WithScanSubdirs`。实现支持解析 `SKILL.md` YAML front matter 中的 `name`、`description`，保留正文 markdown、目录和更新时间；默认只加载当前目录，显式开启后扫描子目录；缺失必填字段的 skill 会被跳过。验证覆盖本地加载、子目录扫描和无效文件跳过。

**阶段五执行记录（2026-05-30）**：已落地 `workspace/` 基础包与 `LocalWorkspace`。当前实现包含 `Workspace`、`MCPClient` 最小接口、workspace ID、生命周期、初始化 `data/`、`skills/`、`sessions/`、内置工具列表、skill 种子复制、skill 列表、内存态 MCP 增删、context JSONL offload、tool result 文本 offload，以及 base64 DataBlock 到 `file://` URL 的本地文件卸载。Docker/E2B、MCP gateway 和持久化 MCP 配置暂未实现。

**阶段五执行记录（2026-05-30）**：已落地剩余 Provider 的第一批可用实现。`model/deepseek`、`model/dashscope`、`model/moonshot`、`model/xai` 复用 `github.com/openai/openai-go`，通过默认 BaseURL 和 provider name 封装 OpenAI-compatible ChatModel；`model/openai` 增加 provider name 透传，保证兼容 Provider 的 `Name()` 与错误归一化使用真实 Provider 名。`model/ollama` 使用官方 `github.com/ollama/ollama/api` `v0.24.0` stable latest 实现 ChatModel，覆盖非流式、流式、参数映射、工具 schema 转换、近似 token 计数和 provider 错误归一化；项目 Go 基线同步升级为 1.26.3。Gemini Provider 仍未实现，后续按 Google Gen AI SDK 单独推进。

**阶段五安全门禁记录（2026-05-30）**：用户确认保留 Ollama 官方 SDK，并决定直接忽略 `github.com/ollama/ollama` 当前 `Fixed in: N/A` 的 govulncheck 阻塞，不做额外过滤和监控。`pre-commit` 不再运行 govulncheck；`make security-check` 也不再把 govulncheck 作为门禁。`make govulncheck` 保留为可手动查看输出的非阻塞目标。

**阶段五 example 执行记录（2026-05-30）**：已落地 `example/` 独立示例矩阵，覆盖 `message`、`model/providers`、`model/dashscope`、`agent/basic`、`tool/function`、`tool/builtin`、`tool/task`、`tool/skill`、`workspace/local`。每个示例子目录都包含独立 `go.mod`、`main.go`、`go.sum`、英文 `README.md` 与中文 `README-zh.md`，代码可重复，不抽公共示例包。用户已删除 `test/examples`，示例验证改为逐目录执行 `go run .`。示例编写原则是按模块用途展示用户第一体感，而不是给每个模块强行套同一种模型调用：`message` 展示 system/user/assistant 对话历史；`model/providers` 展示已实现 Provider 构造与本地 token 估算；`model/dashscope` 覆盖文本调用、`GetWeather` tool call、工具执行、`ToolResultBlock` 回填和最终模型回复；`agent/basic` 用 scripted ChatModel 展示 Agent + task tool 的完整 ReAct 流程；`tool/function`、`tool/builtin`、`tool/task` 分别演示 DashScope 请求 `Greet`、`Read`、`TaskGet` 后由 Go 本地执行工具并回填结果，live 路径使用最多 4 轮的 tool-call 循环，直到模型返回最终文本；`tool/skill` 展示本地 `SKILL.md` 资源加载；`workspace/local` 展示 LocalWorkspace 作为 AI 工具运行环境，工具从 workspace 暴露并在 workspace `data/` 目录读写，同时覆盖 skill、context offload 和 tool result offload。

**阶段五后续 TODO：workspace 与多模态能力缺口（2026-05-30）**：当前 Go 版已有 `workspace/` 基础包和 LocalWorkspace，但 workspace 后端隔离、MCP gateway、Gemini、多模态输出/视频输入、embedding 和独立 TTS/Image/Video 生成模型仍未落地。Python 版已存在完整 workspace 抽象与 Local/Docker/E2B 实现。多模态方面，Go 版已经具备 `message.DataBlock` 协议和部分 Provider 输入适配，但没有完整的 Python Provider 覆盖，也没有独立 TTS、图像生成、视频生成模型抽象。后续按下表推进。

| 能力 | Python 版本状态 | Go 当前状态 | 后续 TODO |
|---|---|---|---|
| Workspace 基础契约 | `WorkspaceBase` 定义 `initialize`、`close`、`reset`、`get_instructions`、`list_tools`、`list_mcps`、`list_skills`、`offload_context`、`offload_tool_result`、动态 MCP/skill 增删；`Offloader` 协议独立存在 | 已实现 Go `workspace.Workspace` 基础接口、workspace ID、生命周期、工具/skill/MCP 列表和 offload 方法；尚未拆出独立 `Offloader` 协议类型 | 后续补独立 `Offloader` 接口、Agent 接入边界和 workspace contract tests |
| LocalWorkspace | Python 版直接暴露本地 Bash/Edit/Glob/Grep/Read/Write，并负责 MCP、skill、offload 数据管理 | 已实现 LocalWorkspace 基础能力：初始化目录、内置工具列表、skill 种子复制/列表、内存态 MCP 增删、context/tool result offload、base64 DataBlock 文件卸载 | 后续补 `.mcp` / `.skills` 持久化索引、手动目录 reconcile、更多 DataBlock 媒体类型和 Agent 集成 |
| DockerWorkspace / E2BWorkspace | Python 版存在容器与 E2B sandbox 后端，通过 workspace 级隔离和 MCP gateway 暴露工具 | 未实现 | 在 LocalWorkspace 稳定后补 `docker.go`、`e2b.go`、gateway client 和 bootstrap；是否引入 Docker/E2B 依赖需单独做 SDK/依赖核查 |
| App workspace 管理层 | Python `app` 下存在 Local/Docker/E2B workspace manager、router 和 service | Go 版尚未实现 `app/` | 作为阶段六以后可选能力；如果 Go 版不提供 HTTP app 层，需要在计划中明确删除或降级原因 |
| Embedding 包 | Python 有 `embedding/`：OpenAI/Gemini/Ollama/DashScope 文本 embedding、DashScope 多模态 embedding、缓存接口和文件缓存 | Go 版只有 `types.Embedding []float64`，没有 `embedding/` 包、模型接口、响应、usage 或缓存 | 新增 `embedding/` 阶段，翻译 `EmbeddingModel`、`EmbeddingResponse`、`EmbeddingUsage`、缓存接口；优先文本 embedding，再补 DashScope 多模态 embedding |
| Chat 多模态输入 | Python formatter 覆盖 Provider 差异：OpenAI 支持图像/音频输入，Gemini 支持图像/音频/视频输入，DashScope 支持图像/音频/视频输入，Anthropic/Ollama/XAI/Moonshot 具备各自图像或音频边界 | Go 版有 `message.DataBlock`；OpenAI formatter 支持 base64 图像、URL 图像和 base64 音频输入；Anthropic formatter 只支持图像并拒绝音频；Ollama 支持 base64 image 输入；DashScope/XAI/Moonshot 当前走 OpenAI-compatible 文本/工具路径；Gemini 未实现，未覆盖视频输入 | Provider 补齐时同步做能力矩阵和契约测试；Gemini/DashScope 需要显式覆盖 video 输入；Anthropic 保持 image-only 并测试拒绝路径 |
| Chat 音频输出 / TTS 类场景 | Python OpenAI Chat 与 DashScope Omni 解析 `audio.data`，输出 `DataBlock(audio/*)`，并可保留 transcript；这不是独立 TTS 抽象，而是 ChatModel 音频输出 | Go OpenAI Provider 目前只解析文本和工具调用，不解析音频输出；DashScope 当前复用 OpenAI-compatible 基础 ChatModel，尚未补 Omni 音频输出；没有 TTS 独立接口 | 先在 OpenAI/DashScope Chat Provider 补音频输出解析和流式 DataBlock 事件；是否新增独立 TTS 接口需另行决策，不能假定 Python 已有同名抽象 |
| 独立 TTS 模型 | Python `model/__init__.py` 未导出独立 TTS/Speech 模型类；当前音频输出挂在 ChatModel Provider 能力上 | 未实现 | 不作为 Python 翻译缺口立即实现；若产品需要独立 TTS，需新增 Go 原生设计和独立阶段 |
| 独立图像生成模型 | Python `model/` 未发现独立 ImageGeneration 模型抽象 | 未实现 | 不作为 Python 翻译缺口立即实现；如需接入图像生成，应先补能力需求、SDK 选择和响应数据结构设计 |
| 独立视频生成模型 | Python `model/` 未发现独立 VideoGeneration 模型抽象；但 DashScope 多模态 embedding 支持 video 输入，Gemini/DashScope chat formatter 支持 video 输入 | 未实现 | 优先补“视频作为输入”的 chat/embedding 能力；独立视频生成模型另行立项 |
| MCP/Tool 多模态内容 | Python tool adapter 可处理 MCP `ImageContent`、`AudioContent` 并转为数据块 | Go `tool/mcp/` 尚未实现 | 实现 `tool/mcp/` 时必须覆盖文本、图像、音频内容映射，以及工具结果中的 DataBlock 合并语义 |

---

## 9. 已知挑战与方案

### 9.1 深拷贝

Python 大量使用 `copy.deepcopy()`。Go 需要在所有需要的类型上实现 `Clone()` 方法。

### 9.2 Pydantic `extra="allow"`

ThinkingBlock 和 ToolCallBlock 接受未知字段。需要自定义 `UnmarshalJSON` 捕获到 `map[string]any`。

### 9.3 `Union[str, list]` 类型

`ToolResultOutput` 可以是字符串或块列表。用封装结构体 `ToolResultOutput{Raw string; Blocks []ContentBlock}` 处理。

### 9.4 `asyncio.gather(return_exceptions=True)`

用 `errgroup.Group` + 错误收集数组，每个 goroutine 始终返回 nil 以避免取消。

### 9.5 Jinja2 模板

用 `text/template` + 自定义 FuncMap 替代。

### 9.6 SSE 流式解析

流式能力优先使用 SDK 暴露的 stream iterator、event channel 或回调。只有 MCP、本地 mock server 或 SDK 明确要求调用方处理低层 SSE 时，才允许在 `internal/` 下写最小解析器；Provider 生产路径不得直接依赖手写 SSE。

### 9.7 Workspace 与多模态能力边界

`workspace/` 是执行环境、资源和 offload 的生命周期管理层，不等同于内置工具本身。Go 当前已具备 LocalWorkspace 基础能力，但还没有 workspace 级 Docker/E2B 隔离、MCP gateway、持久化 MCP 配置和 Agent 自动接入；因此涉及隔离后端和 gateway 的计划项仍按“未实现”处理。

多模态能力分三层记录，避免把不同能力混在一起：

- **消息协议层**：Go 已有 `message.DataBlock`、`Base64Source`、`URLSource` 和数据事件，可承载图像、音频、视频等二进制或 URL 数据。
- **Chat Provider 层**：Go 已实现 OpenAI/Anthropic、DeepSeek/DashScope/Moonshot/XAI 兼容封装和 Ollama；OpenAI 支持图像和音频输入格式化，Anthropic 只支持图像，Ollama 支持 base64 image 输入；音频输出、视频输入、Gemini 和 DashScope Omni 特化能力仍未补齐。
- **独立模型层**：Python 版没有独立 TTS、图像生成或视频生成模型抽象；Python 已有的是 ChatModel 的音频输出能力和 `embedding/` 的多模态 embedding。Go 后续若新增独立 TTS/Image/Video 生成模型，应作为新设计而不是 Python 翻译遗漏。

---

## 10. 测试策略

| 范围 | 测试类型 | 关键场景 |
|---|---|---|
| `permission/` | 单元测试 | 规则匹配、引擎决策、模式切换 |
| `message/` | 单元测试 | JSON 往返、工厂函数、Clone()、ApplyEvent |
| `agent/` | 单元 + 集成 | ReAct 循环、上下文压缩、中间件链 |
| `model/` | 单元测试（mock HTTP） | SSE 解析、重试、流式输出、Token 计数 |
| `tool/` | 单元 + 集成 | 注册、执行、并发、错误处理 |
| 根包 facade | 兼容性测试 | alias API 与领域包类型一致、根目录无领域单测 |

测试分三层：

- **包内单元测试**：每个包保留 `_test.go`，优先使用标准库 `testing`、表驱动测试和 `go-cmp`。适合纯函数、JSON 编解码、权限规则、工具参数校验。
- **跨包集成测试**：参考 Kueue 的 `test/integration/<scope>/suite_test.go`，使用 Ginkgo/Gomega 组织 Agent ReAct、工具链、模型契约等跨包场景。
- **facade 兼容性测试**：根包不放 `_test.go`。需要验证根包 alias 时放入 `test/package_structure/`，避免把领域单测重新拉回根目录。
- **全局端到端冒烟测试**：放在 `test/e2e/global/`，默认使用本地 fake model、本地工具和 Agent 状态执行完整闭环，纳入 `go test ./...`，不依赖外部模型。
- **Provider 端到端冒烟测试**：放在 `test/e2e/provider/`，默认跳过真实外部模型调用；只有显式设置环境变量（例如 `AGENTSCOPE_E2E_MODEL=openai` 和对应 API Key）才运行。

使用 `httptest.NewServer` 只模拟 SDK 调用的远端服务，不把模拟协议反向变成生产实现。模型 Provider 的生产代码必须通过 SDK client 注入、SDK transport 注入或 SDK 提供的测试 hook 完成可测性。

---

## 11. Python 到 Go 转换注意事项

### 11.1 语法与类型系统

| Python 写法 | Go 转换策略 | 注意点 |
|---|---|---|
| `async def` / async generator | `context.Context` + yield 回调 | 回调返回 error 作为背压和中断信号；不要默认开 goroutine |
| `asyncio.gather(return_exceptions=True)` | `errgroup.Group` + 结果数组 | 需要保留部分成功结果时，goroutine 内收集错误，不让 errgroup 过早取消 |
| `typing.Union` / `Literal` | 接口、枚举常量、封装结构体 | JSON 解码必须先探测 type 字段，再进入具体类型 |
| `pydantic.BaseModel` | struct + 构造函数 + `Validate()` | Go 零值要有明确语义，不能依赖 Pydantic 默认值自动补齐 |
| `extra="allow"` | `Extra map[string]any` + 自定义 JSON | 未知字段应往返保留，但核心逻辑不得依赖未知字段 |
| `Optional[T]` | `*T` 或零值 + `HasX` 标记 | 区分“未设置”和“设置为空字符串/0/false” |
| `dict[str, Any]` | `map[string]any` 或强类型结构 | 边界层可用 map，核心层应尽快转强类型 |
| `copy.deepcopy()` | `Clone()` | slice、map、指针字段必须深拷贝；测试覆盖修改副本不影响原值 |
| `isinstance()` | type switch | 接口必须有未导出标记方法，防止外部伪实现破坏穷尽性 |
| `try/except` | `errors.Is`、`errors.As`、包装错误 | 错误类型要可判定，不只拼接字符串 |
| `dataclass` / 动态属性 | struct + Option 函数 | 可选参数用 `WithXxx`，避免构造函数参数过长 |
| `yield` 事件流 | `func(Event) error` | 所有事件必须有稳定 ID、时间和 reply ID，便于回放 |

### 11.2 行为语义

- Python 允许运行时 monkey patch 和延迟 import；Go 版本通过接口注入和包边界表达扩展点，不保留 monkey patch 行为。
- Python 的列表、字典是引用语义；Go slice、map 也是引用底层数据，公开返回前必须复制，尤其是 message content、metadata、permission rules。
- Python 的 `None` 与空列表、空字符串常混用；Go API 必须明确区分 nil、空集合和默认值，序列化时用 `omitempty` 保持兼容。
- Python 的异常可以跨层透传；Go 中模型、工具、权限、开发者错误必须分层，便于调用方用 `errors.As` 分支处理。
- Python 的动态导入适合可选依赖；Go 中可选 Provider 应放独立子包，用户只有 import 对应包时才引入 SDK 依赖。
- Python 的上下文对象可随意挂属性；Go 中 AgentState、ToolContext、TaskContext 要定义清晰字段，扩展数据放 `Metadata map[string]any`。

### 11.3 JSON 与流式事件

- 所有带 `type` 字段的 block、event、data source 都用两步 JSON 解码：先读判别字段，再解具体结构。
- Provider 特有字段只允许进入 `Extra`，不得污染公共结构；公共结构变更需要同步模型 formatter、event apply 和测试 fixture。
- 流式 delta 应先在 provider adapter 中归一化，再进入 Agent ReAct 层。Agent 层只处理框架事件，不处理 SDK chunk。
- 消息回放测试必须覆盖“事件序列 -> Message”与“Message -> JSON -> Message”两个方向，避免 Python 版事件协议在 Go 中失真。

### 11.4 包结构转换规则

- 根包只放兼容 facade：通过 type alias / var alias 重新导出高频 API，不承载实现、协议契约、业务逻辑或领域单测。
- `agent/` 承载 Agent、Config、ReAct 推理-执行循环、中间件 hook 链和 Agent 相关窄接口；根包不得新增 `agent.go`、`reasoning.go`、`acting.go` 等实现文件。
- `state/` 承载 AgentState、ToolContext、TaskContext、Task；`model/` 承载 ChatModel、CallRequest、ChatResponse、ToolSchema；`tool/` 承载 Tool、ToolChunk、ToolResponse、Toolkit、FunctionTool、ResetTools。
- `message/`、`permission/`、`types/`、`errors/` 是基础数据包，禁止反向导入根包。
- `utils/` 只放跨包复用、无业务依赖的轻量工具函数；复杂能力、测试辅助或内部适配仍放 `internal/`。
- `model/<provider>/`、`tool/builtin/`、`middleware/` 是实现包，只能依赖领域包接口，不得依赖根包 facade，也不得把实现反注入根包。
- 面向消费者的窄接口应定义在使用方领域包内，例如阶段四的 `agent.ToolProvider` 由 `tool.Toolkit` 实现；根包仅重新导出，避免 `root -> tool -> root` 循环。
- 内置工具只使用 `tool/builtin/` 和 `package builtin`；禁止新建 `tool/native/` 或 `tools/native/`，避免与 Provider 原生能力命名冲突。
- `internal/` 放不可导出的通用能力，例如 JSON 修复、日志、测试辅助和 SDK adapter 内部工具。
- `test/` 放跨包集成测试和根 facade 兼容性测试；包内 `_test.go` 与实现同目录，根目录不放 `_test.go`。
- Go 源码注释统一使用英文，包括包注释、导出标识符注释、测试辅助类型注释和必要实现说明；翻译计划、review、README 等中文文档不受此限制。
- 不为 Python 的每个文件创建 Go 包。只有当依赖方向、可选依赖或用户导入路径需要隔离时才建子包。

---

## 12. 测试框架落地计划

### 12.1 参考 Kueue 的目录分层

Kueue 的可复用经验是：包内单元测试贴近实现，跨包测试放 `test/`，通用测试能力集中到 `test/util` 或内部 testing 包，集成测试用 `suite_test.go` 建立统一初始化和清理流程。AgentScope-Go 采用简化版：

```
agentscope-go/
├── internal/testutil/
│   ├── cmp.go              # cmpopts、时间字段忽略、JSON 标准化
│   ├── event.go            # 事件收集器、事件序列断言
│   └── model.go            # fake ChatModel、脚本化模型响应
├── test/
│   ├── util/
│   │   ├── constants.go    # Timeout、Interval、测试环境变量名
│   │   ├── fixtures.go     # Python 协议 fixture、消息 fixture
│   │   └── mockserver.go   # SDK mock server、流式响应脚本
│   ├── package_structure/
│   │   └── package_structure_test.go
│   ├── integration/
│   │   ├── agent/
│   │   ├── model/
│   │   └── tool/
│   └── e2e/
│       ├── global/
│       └── provider/
```

### 12.2 测试依赖

| 依赖 | 使用位置 | 引入阶段 |
|---|---|---|
| `github.com/google/go-cmp/cmp` | 单元测试结构体比较、忽略时间字段 | 阶段一 |
| `github.com/onsi/ginkgo/v2` | `test/integration/**/suite_test.go` | 阶段二 |
| `github.com/onsi/gomega` | 集成测试断言、Eventually | 阶段二 |
| `go.uber.org/mock` | 生成 ChatModel、Tool、Middleware mock | 阶段二 |
| `golang.org/x/sync/errgroup` | 并发工具执行与测试验证 | 阶段三 |

### 12.3 Makefile 验证目标

参考 Kueue 的 Makefile 分层，建议新增：

| 目标 | 命令 | 用途 |
|---|---|---|
| `make fmt` | `go fmt ./...` | 格式化 |
| `make vet` | `go vet ./...` | 静态检查 |
| `make test-unit` | `go test ./... -run 'Test'` | 快速单元测试 |
| `make test-integration` | `ginkgo -r ./test/integration` | 跨包集成测试 |
| `make test-e2e` | `go test ./test/e2e/...` | 全局 E2E 默认执行；真实 Provider 冒烟默认需要环境变量 |
| `make coverage` | `go test ./... -coverprofile=coverage.out` | 覆盖率统计 |
| `make verify` | `fmt vet test-unit test-integration` | 本地总验收 |

### 12.4 覆盖率与用例映射

- `permission/`、`message/`、`internal/jsonutil/` 目标覆盖率不低于 90%，因为它们是协议兼容基础。
- `model/<provider>/` 每个 Provider 必须有统一契约测试：普通响应、流式响应、工具调用、错误映射、上下文取消、结构化输出。
- `tool/builtin/` 必须覆盖权限拒绝、危险路径、只读工具、并发执行和错误输出。阶段三已通过包内测试覆盖 Read/Write/Edit 缓存与路径约束、Bash 危险命令、Glob/Grep 搜索和 ResetTools 状态更新；后续 Agent 阶段继续补工具并发调度集成测试。
- Agent ReAct 集成测试必须覆盖：纯文本回复、工具调用、权限询问、工具失败恢复、上下文压缩、中间件拦截。
- 全局 E2E 必须覆盖至少一条 Agent → model → tool → state → model 的完整闭环，使用本地 fake model，纳入默认本地测试。
- Provider E2E 只验证 SDK 与真实服务的最小闭环，不把外部模型稳定性纳入默认 CI 门槛。

---

## 13. 第三方模型 SDK 接入策略

### 13.1 接入原则

- 每个 Provider 的生产代码必须通过 SDK client 完成请求、鉴权、流式读取和基础错误处理。
- 框架层只做三件事：把 `message.Message` 转为 SDK 入参，把 SDK 返回转为 `ChatResponse` 或 `message.Event`，把 SDK 错误转为框架错误。
- OpenAI 兼容 Provider 不复制 OpenAI SDK 内部结构；只配置 BaseURL、API Key、默认 Header 和模型能力矩阵。
- SDK 版本写入 `go.mod` 后必须记录在本节依赖表中，并为破坏性升级建立 Provider 契约测试。
- 若某 Provider 没有稳定 Go SDK，先标为“暂缓”，不得退回自研 HTTP Provider；只有 MCP、本地工具和内部 mock 可直接使用 `net/http`。

### 13.2 推荐模块清单

| 能力 | 模块 | 当前核验结果 | 备注 |
|---|---|---|---|
| OpenAI | `github.com/openai/openai-go` | 已接入 `v1.12.0` | 优先用于 OpenAI、DeepSeek、XAI、Moonshot/Kimi 兼容接口 |
| Anthropic | `github.com/anthropics/anthropic-sdk-go` | 已接入 `v1.46.0` | 支持 Claude 相关能力，适配 thinking/tool use |
| Gemini | `google.golang.org/genai` | Go 模块存在，已发布 v1.x | Google Gen AI SDK |
| DashScope | `github.com/openai/openai-go` | Go 模块存在，已发布 v1.x | 通过 DashScope OpenAI 兼容接口接入，不使用阿里云专用 SDK |
| Ollama | `github.com/ollama/ollama/api` | 已接入 `github.com/ollama/ollama` `v0.24.0` | 导入路径位于 `github.com/ollama/ollama` 模块内；项目 Go 基线已升级到 1.26.3，可使用当前 stable latest |
| MCP | `github.com/mark3labs/mcp-go` | 计划沿用 | MCP 协议客户端和工具适配 |

### 13.3 Provider 契约

每个 Provider 子包必须实现以下契约测试：

- `Call` 接收同一组 `message.Message`，在非流式模式返回等价 `ChatResponse`。
- `Call` 在流式模式输出稳定事件顺序：开始、delta、结束、usage。
- 工具调用从 SDK 格式转成 `ToolCallBlock`，工具结果再能被 formatter 转回 SDK 输入。
- 结构化输出使用 SDK 原生参数；SDK 不支持时返回明确 `ErrUnsupportedCapability`。
- `context.Context` 取消必须能中断 SDK 调用并返回可判定错误。
- 429、401/403、上下文超限、服务端 5xx 等错误必须映射为框架错误类型。

---

## 14. 证据来源与验收清单

### 14.1 本地证据

| 证据 | 来源 | 结论 |
|---|---|---|
| 当前 Go 模块 | `go.mod` | 模块路径为 `github.com/yuluo-yx/agentscope-go`，阶段二已引入 OpenAI、Anthropic 等 Provider SDK |
| Python 源码结构 | `../agentscope/src/agentscope` | Python 版包含 agent、message、event、model、tool、permission、middleware、workspace 等模块 |
| Python 内置工具目录 | `../agentscope/src/agentscope/tool/_builtin` | Python 版内置工具使用 `_builtin` 命名，对外导出 Bash、Read、Write、Edit、Glob、Grep、ResetTools、SkillViewer；Go 版对应 `tool/builtin/` |
| Python 本地工具执行 | `../agentscope/src/agentscope/workspace/_local_workspace.py`、`../agentscope/src/agentscope/tool/_builtin/_bash.py` | `LocalWorkspace.list_tools()` 直接暴露本地内置工具；`Bash.__call__` 使用 `asyncio.create_subprocess_shell` 在宿主环境执行，没有额外本地工具沙箱 |
| Python workspace 隔离 | `../agentscope/src/agentscope/workspace/_docker/_docker_workspace.py`、`../agentscope/src/agentscope/workspace/_e2b/_e2b_workspace.py` | Docker/E2B 是 workspace 级隔离，内置 `list_tools()` 返回空列表，工具经容器或 sandbox 内的 MCP gateway 暴露 |
| Python workspace 契约 | `../agentscope/src/agentscope/workspace/_base.py`、`../agentscope/src/agentscope/workspace/_offload_protocol.py`、`../agentscope/src/agentscope/workspace/__init__.py` | Python 版导出 `WorkspaceBase`、`LocalWorkspace`、`DockerWorkspace`、`E2BWorkspace`、`Offloader`，覆盖生命周期、资源发现、MCP/skill 动态管理和 offload |
| Go skill 加载 | `tool/skill/skill.go`、`tool/skill/loader.go`、`tool/skill/loader_test.go` | 阶段五已落地本地 skill 加载，支持 `SKILL.md` front matter、正文 markdown、子目录扫描和无效 skill 跳过；验证命令为 `go test ./tool/skill -count=1` |
| Go workspace 基础实现 | `workspace/workspace.go`、`workspace/local.go`、`workspace/local_test.go` | 阶段五已落地 `Workspace` 接口和 LocalWorkspace 基础能力；验证覆盖初始化目录、内置工具列表、skill 种子加载、context/tool result offload 和 base64 DataBlock 文件卸载；Docker/E2B 与 MCP gateway 仍未实现 |
| Python 共享类型模块 | `../agentscope/src/agentscope/types` | Python 版 `types` 顶层模块导出 JSONPrimitive、JSONSerializableObject、Embedding、Hook 类型；Go 版对应 `types/` |
| Python 异常模块 | `../agentscope/src/agentscope/exception` | Python 版 `exception` 顶层模块导出 AgentOrientedException、DeveloperOrientedException 和工具错误；Go 版对应 `errors/` |
| Python 模型与多模态 | `../agentscope/src/agentscope/model/__init__.py`、`../agentscope/src/agentscope/formatter`、`../agentscope/src/agentscope/model/_openai_chat/_model.py`、`../agentscope/src/agentscope/model/_dashscope/_model.py` | Python `model` 导出 ChatModel Provider，不导出独立 TTS/Image/Video 生成模型；OpenAI/DashScope Chat 支持音频输出解析，多个 formatter 支持图像、音频或视频输入 |
| Python embedding | `../agentscope/src/agentscope/embedding/__init__.py`、`../agentscope/src/agentscope/embedding/_dashscope_multimodal_embedding.py` | Python 版有独立 embedding 包；DashScope 多模态 embedding 支持 text、image、video |
| Go Provider 与多模态现状 | `message/block.go`、`model/openai/formatter.go`、`model/openai/chat_model.go`、`model/anthropic/formatter.go`、`model/deepseek/`、`model/dashscope/`、`model/moonshot/`、`model/xai/`、`model/ollama/`、`types/types.go` | Go 版已有 DataBlock 协议、OpenAI/Anthropic、OpenAI-compatible Provider wrappers 和 Ollama Provider；OpenAI 当前只解析文本和工具调用输出，Anthropic 拒绝音频，Ollama 支持 base64 image 输入；没有 `embedding/` 包，也没有独立 TTS/Image/Video 生成模型接口 |
| Go 深拷贝辅助 | `utils/clone.go`、`message/block.go` | 领域包共享 `CloneAnyMap` / `CloneAny`，`message.ContentBlockList.Clone()` 统一内容块深拷贝语义 |
| Go 工具系统 | `tool/`、`tool/builtin/`、`go.mod` | 阶段三已落地 Toolkit、ToolGroup、FunctionTool 与 Bash/Read/Write/Edit/Glob/Grep/ResetTools；Bash parser 使用 `mvdan.cc/sh/v3` `v3.13.1`；验证命令为 `go test ./tool/...` |
| Go Agent 核心 | `agent/agent.go`、`agent/reasoning.go`、`agent/acting.go`、`agent/context.go`、`agent/middleware_chain.go`、`agent/agent_test.go` | 阶段四已落地 NewAgent、Reply/ReplyStream、模型推理、工具执行、权限确认、中间件 hook 链、concurrency-safe 工具并发批处理和工具结果截断；验证命令为 `go test ./agent -run 'TestAgent' -count=1` |
| Go 根包 facade | `facade.go`、`test/package_structure/package_structure_test.go` | 根包只保留 alias facade；兼容性测试放在 `test/package_structure/`，根目录不保留 `_test.go` |
| Python 任务工具 | `../agentscope/src/agentscope/tool/_task`、`../agentscope/src/agentscope/state/_task.py` | Python 版导出 `TaskCreate`、`TaskGet`、`TaskList`、`TaskUpdate`；任务 ID 使用 `uuid.uuid4().hex`；状态字段为 `pending`、`in_progress`、`completed` |
| Go 任务工具与全局 E2E | `tool/task/`、`state/task.go`、`test/e2e/global/smoke_test.go` | 阶段五已落地任务工具和本地全局 E2E；Go 版同步 Python 32 位 hex 任务 ID，并记录写工具 concurrency-safe 标记差异 |
| Python 测试结构 | `../agentscope/tests` | 测试覆盖 message/event、formatter、model、tool、permission、middleware、workspace、MCP、Redis |
| Kueue 测试结构 | `/Users/shown/workspace/golang/kueue/test`、`/Users/shown/workspace/golang/kueue/pkg/util/testing` | 可参考 `test/util`、内部 testing helper、`test/integration/**/suite_test.go` 分层 |
| Kueue 测试依赖 | `/Users/shown/workspace/golang/kueue/go.mod` | 使用 Ginkgo/Gomega、go-cmp、gomock、errgroup 等 Go 测试生态 |
| SDK 模块核验 | `go list -m -versions ...` | OpenAI、Anthropic、Gemini、Ollama 相关 Go 模块均可解析版本；DashScope 复用 OpenAI SDK |

### 14.2 翻译验收清单

- 包依赖图保持单向：基础包支撑领域包；实现包和根包 facade 都只依赖领域包；禁止实现包导入根包 facade。
- 每个 Python 模块都有明确 Go 去向；未翻译能力要在阶段计划中说明原因。
- 每个导出的 Go 类型都有稳定构造方式、JSON 行为和错误语义。
- Provider 生产代码不手写第三方模型 HTTP 协议，全部走 SDK 或兼容 SDK。
- Python fixture 必须覆盖 JSON 兼容性，确保 Go 版消息、事件、工具调用能读写 Python 版协议。
- 每次发现 Go 实现与 Python 语义、文件布局或命名存在出入时，必须优先修正实现；若属于 Go 生态或并发安全导致的有意差异，必须在对应阶段记录中说明原因、影响范围和验证方式。
- 每个阶段完成后运行 `go test ./...`；阶段二以后按当前已落地目标运行对应本地验证，集成测试框架落地后再启用 `make test-integration`。
- 覆盖率目标：核心协议包不低于 90%；Provider 契约和 Agent ReAct 主路径必须有集成测试。

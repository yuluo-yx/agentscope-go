package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	asw "github.com/yuluo-yx/agentscope-go/workspace/agentsandbox"
)

func main() {

	ctx := context.Background()

	// 创建一个临时的 workspace 目录
	rootDir, err := os.MkdirTemp("", "agentscope-agent-sandbox-workspace-example-*")
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = os.RemoveAll(rootDir)
	}()

	// hostWorkdir 是宿主机 mirror 目录，用来存放 offload、skills 和 MCP 索引。
	hostWorkdir := filepath.Join(rootDir, "workspace")
	ws, err := asw.NewWorkspace(agentSandboxOption(hostWorkdir)...)
	if err != nil {
		panic(err)
	}

	// 初始化 workspace
	// agent.WithWorkspace 在检测到 ws 未初始化之后，会自动调用初始化。
	err = ws.Initialize(ctx)
	if err != nil {
		panic(err)
	}
	// 探活
	alive := ws.IsAlive()
	if !alive {
		panic("agent sandbox is alive")
	}
	fmt.Printf("agent sandbox is alive: %v. \n", alive)

	// defer 释放资源
	defer func() {
		if err := ws.Close(ctx); err != nil {
			panic(err)
		}
	}()

	// 列举下 workspace 中的工具
	// 在实际使用 agent 时，通过 WithWorkspace 会自动注册。
	tools, err := ws.ListTools(ctx)
	if err != nil {
		panic(err)
	}
	for _, t := range tools {
		fmt.Println("tools: ", t.Name())
	}
	fmt.Println()

	/**
	output:
		agent sandbox is alive: true.
		tools:  Bash
		tools:  Edit
		tools:  Glob
		tools:  Grep
		tools:  Read
		tools:  Write
	*/

	// 调用 Bash tools 创建一个文件 然后用 Write 并写入内容，用 Edit 编辑，最后使用 Read 读取文件内容。

	chatModel := newChatModel(true)
	chat(ctx, chatModel, ws)

	// 如果一切顺利，在 go run . 执行完成时，将看到类似输出
	/**
	好的，我来按步骤完成这个任务！

	## 步骤 1 & 2：创建文件并写入内容

	[tool-call] Write
	{"content": "Hello, AgentScope Go.\n", "file_path": "/home/user/demo/demo.md"}
	[tool-result-start] Write
	The file /home/user/demo/demo.md has been written successfully inside the Agent Sandbox workspace (2 lines).
	[tool-result-end] state=success
	文件创建成功！接下来同时进行步骤 3（Grep 搜索）和步骤 4（Read 读取）：


	[tool-call] Grep
	{"path": "/home/user", "pattern": "AgentScope"}
	[tool-call] Read
	{"file_path": "/home/user/demo/demo.md"}
	[tool-result-start] Grep

	[tool-result-start] Read
	/home/user/demo/demo.md:1:Hello, AgentScope Go.
	[tool-result-end] state=success
	     1  Hello, AgentScope Go.
	[tool-result-end] state=success
	## 任务完成 ✅

	以下是最终结果：

	| 项目 | 结果 |
	|------|------|
	| **文件路径** | `/home/user/demo/demo.md` |
	| **文件内容** | `Hello, AgentScope Go.` |

	### 各步骤执行摘要：

	1. **创建文件** — 通过 Write 工具成功创建 `/home/user/demo/demo.md`（2 行，含末尾换行）。
	2. **写入内容** — 内容 `Hello, AgentScope Go.` 已写入文件。
	3. **Grep 搜索** — 搜索 `"AgentScope"` 匹配到 `/home/user/demo/demo.md` 第 1 行，确认文件路径正确。
	4. **Read 读取** — 读取到第 1 行内容为 `Hello, AgentScope Go.`，与写入一致。

	agent_final="好的，我来按步骤完成这个任务！\n\n## 步骤 1 & 2：创建文件并写入内容\n\n文件创建成功！接下来同时进行步骤 3（Grep 搜索）和步骤 4（Read 读取）：\n\n## 任务完成 ✅\n\n以下是最终结果：\n\n| 项目 | 结果 |\n|------|------|\n| **文件路径** | `/home/user/demo/demo.md` |\n| **文件内容** | `Hello, AgentScope Go.` |\n\n### 各步骤执行摘要：\n\n1. **创建文件** — 通过 Write 工具成功创建 `/home/user/demo/demo.md`（2 行，含末尾换行）。\n2. **写入内容** — 内容 `Hello, AgentScope Go.` 已写入文件。\n3. **Grep 搜索** — 搜索 `\"AgentScope\"` 匹配到 `/home/user/demo/demo.md` 第 1 行，确认文件路径正确。\n4. **Read 读取** — 读取到第 1 行内容为 `Hello, AgentScope Go.`，与写入一致。"

	进入对应的 sandbox 集群，可以看到一个存活的 pod，

	agent                  sandbox-claim-gpfpd                          1/1     Running     0          34s

	cd 到对应位置可以看到一个文件和文件内容：

	I have no name!@sandbox-claim-gpfpd:/home/user/demo$ cat demo.md
	Hello, AgentScope Go.
	I have no name!@sandbox-claim-gpfpd:/home/user/demo$ pwd
	/home/user/demo
	*/
}

func chat(ctx context.Context, chatModel model.ChatModel, ws *asw.Workspace) {

	// agent tool call state config.
	agentState := asstate.NewAgentState()
	agentState.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	agentState.PermissionContext.WorkingDirectories["sandbox-demo"] = permission.AdditionalWorkingDirectory{
		Path:   "/home/user/demo",
		Source: "agentsandbox demo",
	}

	// user prompt 输入
	systemPrompt := strings.Join([]string{
		"You are an AgentScope Go workspace demo agent.",
		"You can use workspace tools to create, write, edit, grep, and read files inside the sandbox.",
		"When the user asks you to create or inspect a file, use the available tools instead of only explaining.",
		"Use Bash only for shell operations, Write for writing file content, Edit for targeted edits, Grep for searching, and Read for reading files.",
		"After finishing tool calls, answer with the final file path and a short summary.",
	}, "\n")

	user, err := message.NewUserMessage(
		"user",
		`请在 sandbox 里完成这个任务：
1. 创建 /home/user/demo/demo.md。
2. 写入一行内容：Hello, AgentScope Go.
3. 使用 Grep 搜索 "AgentScope" 来确认文件路径。
4. 使用 Read 读取 demo.md。
5. 最后告诉我文件路径和读取到的内容。`,
	)
	if err != nil {
		panic(err)
	}

	demoAgent, err := agent.NewAgent(
		"Workspace Demo Agent",
		systemPrompt,
		chatModel,
		agent.WithAgentState(agentState),
		agent.WithWorkspace(ctx, ws),
		agent.WithReActConfig(agent.ReActConfig{
			MaxIters:     10,
			StopOnReject: true,
		}),
	)
	if err != nil {
		panic(err)
	}

	var finalText strings.Builder

	// stream call
	err = demoAgent.ReplyStream(ctx, user, func(event message.Event) error {
		switch e := event.(type) {
		case *message.ToolCallStartEvent:
			fmt.Printf("\n[tool-call] %s\n", e.ToolCallName)
		case *message.ToolCallDeltaEvent:
			fmt.Print(e.Delta)
		case *message.ToolResultStartEvent:
			fmt.Printf("\n[tool-result-start] %s\n", e.ToolCallName)
		case *message.ToolResultTextDeltaEvent:
			fmt.Print(e.Delta)
		case *message.ToolResultEndEvent:
			fmt.Printf("\n[tool-result-end] state=%s\n", e.State)
		case *message.TextBlockDeltaEvent:
			finalText.WriteString(e.Delta)
			fmt.Print(e.Delta)
		case *message.ExceedMaxItersEvent:
			fmt.Printf("\n[agent] exceed max iters: %s\n", e.Name)
		case *message.RequireUserConfirmEvent:
			fmt.Printf("\n[agent] requires user confirmation for %d tool call(s)\n", len(e.ToolCalls))
		}
		return nil
	})
	if err != nil {
		panic(err)
	}

	// final output:
	fmt.Printf("\n\nagent_final=%q\n", finalText.String())
}

// 设置 sandbox 连接参数
func agentSandboxOption(dir string) []asw.Option {

	opts := []asw.Option{
		// 设置模版名称，下面用到的 python-sandbox-template 是集群里已经有的一个 sandboxTemplate 的 crd 名字，可以自己创建
		asw.WithTemplateName("python-sandbox-template"),
		// 设置 ns
		asw.WithNamespace("agent"),
		// 设置宿主机 mirror 目录
		asw.WithHostWorkdir(dir),
		// agent 进程结束时，保留 pod，以查看。
		asw.WithKeepSandbox(true),
	}

	// 只有显式设置 AGENTSCOPE_AGENT_SANDBOX_API_URL 时，才走 direct URL 模式。
	if apiURL := strings.TrimSpace(os.Getenv("AGENTSCOPE_AGENT_SANDBOX_API_URL")); apiURL != "" {
		opts = append(opts, asw.WithAPIURL(apiURL))
	}

	// 只有显式设置 AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAME 时，才走 Gateway 模式。
	// WithGateway 需要两个参数：gateway name 和 gateway namespace。
	if gateway := strings.TrimSpace(os.Getenv("AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAME")); gateway != "" {
		opts = append(opts, asw.WithGateway(
			gateway,
			"agent",
		))
	}

	return opts
}

func newChatModel(stream bool) model.ChatModel {

	dashscopeModel, err := dashscope.NewChatModel(
		dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
		"qwen3.7-max",
		dashscope.WithStream(stream),
	)
	if err != nil {
		panic(err)
	}

	return dashscopeModel
}

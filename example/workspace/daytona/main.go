// Copyright The AgentScope Go Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
	daytonaws "github.com/yuluo-yx/agentscope-go/workspace/daytona"
)

const (
	fixtureCSVPath    = "data/sales.csv"
	fixtureRunnerPath = "python_runner.py"

	sandboxCSVPath      = "/home/daytona/data/sales.csv"
	sandboxRunnerPath   = "/home/daytona/tools/python_runner.py"
	generatedScriptPath = "/home/daytona/generated/analysis.py"
	resultPath          = "/home/daytona/data/analysis_result.json"
	reportPath          = "/home/daytona/data/analysis_report.md"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "daytona workspace example failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	root, err := os.MkdirTemp("", "agentscope-daytona-workspace-example-*")
	if err != nil {
		return fmt.Errorf("create temp workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	keepSandbox, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("AGENTSCOPE_DAYTONA_KEEP_SANDBOX")))
	image := strings.TrimSpace(os.Getenv("AGENTSCOPE_DAYTONA_IMAGE"))
	if image == "" {
		image = "python:3.12"
	}

	ws, err := daytonaws.NewWorkspace(
		daytonaws.WithImage(image),
		daytonaws.WithHostWorkdir(filepath.Join(root, "workspace")),
		daytonaws.WithKeepSandbox(keepSandbox),
		daytonaws.WithRequestTimeout(90*time.Second),
		daytonaws.WithOpenTimeout(4*time.Minute),
	)
	if err != nil {
		return fmt.Errorf("create Daytona workspace: %w", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize Daytona workspace: %w", err)
	}
	defer func() {
		if err := ws.Close(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "close Daytona workspace failed: %v\n", err)
		}
	}()

	tools, err := ws.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("list Daytona workspace tools: %w", err)
	}
	writeTool, err := toolByName(tools, "Write")
	if err != nil {
		return err
	}
	bashTool, err := toolByName(tools, "Bash")
	if err != nil {
		return err
	}

	dir, err := exampleDir()
	if err != nil {
		return err
	}
	writeState := asstate.NewAgentState()
	localCSVPath := filepath.Join(dir, fixtureCSVPath)
	localRunnerPath := filepath.Join(dir, fixtureRunnerPath)
	if err := uploadExampleFile(ctx, writeTool, localCSVPath, sandboxCSVPath, writeState); err != nil {
		return err
	}
	if err := uploadExampleFile(ctx, writeTool, localRunnerPath, sandboxRunnerPath, writeState); err != nil {
		return err
	}
	schema, err := inspectCSV(localCSVPath, fixtureCSVPath, sandboxCSVPath)
	if err != nil {
		return err
	}

	model, err := newChatModel()
	if err != nil {
		return err
	}
	executorTool, err := newPythonAnalysisTool(writeTool, bashTool)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(executorTool)
	if err != nil {
		return fmt.Errorf("create analysis toolkit: %w", err)
	}
	agentState := asstate.NewAgentState()
	agentState.PermissionContext = permission.NewContext(permission.ModeBypass)
	analyst, err := agentpkg.NewAgent(
		"DataAnalyst",
		analysisSystemPrompt(),
		model,
		agentpkg.WithToolkit(kit),
		agentpkg.WithAgentState(agentState),
	)
	if err != nil {
		return fmt.Errorf("create analysis agent: %w", err)
	}
	prompt, err := analysisUserPrompt(schema)
	if err != nil {
		return err
	}
	userMessage, err := message.NewUserMessage("user", prompt)
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}
	reply, err := analyst.Reply(ctx, userMessage)
	if err != nil {
		return fmt.Errorf("run analysis agent: %w", err)
	}

	fmt.Printf("daytona_workspace_alive=%t sandbox_id=%s keep_sandbox=%t model=%s\n",
		ws.IsAlive(),
		ws.SandboxID(),
		keepSandbox,
		model.Name(),
	)
	fmt.Printf("csv_source=%s sandbox_csv=%s generated_python=%s\n", fixtureCSVPath, sandboxCSVPath, generatedScriptPath)
	fmt.Println("agent_conclusion:")
	if text := reply.GetTextContent(""); text != nil {
		fmt.Println(*text)
	}
	return nil
}

func analysisSystemPrompt() string {
	return `You are a data analyst.

When the user asks for CSV analysis:
1. Read the CSV schema and sample rows from the prompt.
2. Generate Python code that uses only the standard library.
3. Call RunPythonAnalysis exactly once with the generated code.
4. The code runs in Daytona. It receives CSV_PATH, RESULT_PATH, and REPORT_PATH globals.
5. The code must write JSON to RESULT_PATH with conclusion, metrics, evidence, and data_source fields.
6. After the tool result, answer in Chinese and include the data source path, row count, and generated Python path.

Do not claim a conclusion before the Python tool has executed.`
}

func analysisUserPrompt(schema csvSchema) (string, error) {
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal csv schema: %w", err)
	}
	return fmt.Sprintf(`帮我分析 CSV，得出“哪个区域的折后销售额最高，以及它领先第二名多少”的结论。

请根据下面的 CSV schema 和样例生成 Python 代码并调用 RunPythonAnalysis 工具。最终回答需要包含：
- 结论
- 关键计算证据
- 数据来源，包括 CSV 路径、行数、生成的 Python 路径、结果文件路径

计算口径：折后销售额 = units * unit_price * (1 - discount)。

CSV schema:
%s`, string(schemaJSON)), nil
}

func newChatModel() (asmodel.ChatModel, error) {
	apiKey := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("AI_DASHSCOPE_API_KEY"))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY or AI_DASHSCOPE_API_KEY is required")
	}
	modelName := strings.TrimSpace(os.Getenv("DASHSCOPE_MODEL"))
	if modelName == "" {
		modelName = "qwen-plus"
	}
	temperature := 0.1
	return dashscope.NewChatModel(
		dashscope.NewCredential(apiKey),
		modelName,
		dashscope.WithChatParameters(dashscope.ChatParameters{Temperature: &temperature}),
	)
}

func newPythonAnalysisTool(writeTool, bashTool asworkspace.Tool) (*tool.FunctionTool, error) {
	executor := &pythonExecutor{
		writeTool: writeTool,
		bashTool:  bashTool,
	}
	return tool.NewFunctionTool(
		"RunPythonAnalysis",
		"Write generated Python into the Daytona sandbox, execute it against the prepared CSV, and return the analysis result with data-source metadata.",
		pythonAnalysisToolSchema(),
		executor.run,
		tool.WithFunctionConcurrencySafe(false),
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{
				Behavior:       permission.BehaviorAllow,
				DecisionReason: "The example grants this single Daytona Python execution tool explicitly.",
			}, nil
		}),
		tool.WithFunctionSuggestedRule("RunPythonAnalysis"),
	)
}

type pythonExecutor struct {
	writeTool asworkspace.Tool
	bashTool  asworkspace.Tool
}

func (e *pythonExecutor) run(ctx context.Context, input map[string]any, state *asstate.AgentState) (message.ContentBlockList, error) {
	code, _ := input["code"].(string)
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("RunPythonAnalysis requires generated Python code")
	}
	if !strings.HasSuffix(code, "\n") {
		code += "\n"
	}
	if err := writeSandboxText(ctx, e.writeTool, generatedScriptPath, code, state); err != nil {
		return nil, err
	}

	command := fmt.Sprintf(
		"python %s --script %s --csv %s --result %s --report %s",
		sandboxRunnerPath,
		generatedScriptPath,
		sandboxCSVPath,
		resultPath,
		reportPath,
	)
	response, err := runWorkspaceTool(ctx, e.bashTool, map[string]any{
		"command":    command,
		"timeout_ms": 90_000,
	}, state)
	if err != nil {
		return nil, err
	}

	output := ""
	if text := response.Content.GetTextContent(""); text != nil {
		output = strings.TrimSpace(*text)
	}
	analysisGoal, _ := input["analysis_goal"].(string)
	payload := map[string]any{
		"analysis_goal":     analysisGoal,
		"csv_path":          sandboxCSVPath,
		"generated_script":  generatedScriptPath,
		"result_path":       resultPath,
		"report_path":       reportPath,
		"tool_result_state": string(response.State),
	}
	if response.State != message.ToolResultSuccess {
		payload["status"] = "execution_error"
		payload["output"] = output
	} else {
		payload["status"] = "ok"
		var runnerPayload map[string]any
		if err := json.Unmarshal([]byte(output), &runnerPayload); err == nil {
			payload["runner_payload"] = runnerPayload
		} else {
			payload["output"] = output
		}
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return message.ContentBlockList{message.NewTextBlock(string(data))}, nil
}

func pythonAnalysisToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []any{"analysis_goal", "code"},
		"properties": map[string]any{
			"analysis_goal": map[string]any{
				"type":        "string",
				"description": "The business question the generated Python code answers.",
			},
			"code": map[string]any{
				"type": "string",
				"description": strings.Join([]string{
					"Complete Python code using only the standard library.",
					"The executor injects CSV_PATH, RESULT_PATH, and REPORT_PATH globals.",
					"The code must write a JSON object to RESULT_PATH with conclusion, metrics, evidence, and data_source fields.",
				}, " "),
			},
		},
	}
}

func uploadExampleFile(ctx context.Context, writeTool asworkspace.Tool, localPath, sandboxPath string, state *asstate.AgentState) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read example file %s: %w", localPath, err)
	}
	return writeSandboxText(ctx, writeTool, sandboxPath, string(data), state)
}

func writeSandboxText(ctx context.Context, writeTool asworkspace.Tool, sandboxPath, content string, state *asstate.AgentState) error {
	response, err := runWorkspaceTool(ctx, writeTool, map[string]any{
		"file_path": sandboxPath,
		"content":   content,
	}, state)
	if err != nil {
		return err
	}
	if response.State != message.ToolResultSuccess {
		output := ""
		if text := response.Content.GetTextContent(""); text != nil {
			output = *text
		}
		return fmt.Errorf("write %s failed: %s", sandboxPath, output)
	}
	return nil
}

func runWorkspaceTool(ctx context.Context, current asworkspace.Tool, input map[string]any, state *asstate.AgentState) (*tool.ToolResponse, error) {
	chunks, err := current.Execute(ctx, input, state)
	if err != nil {
		return nil, err
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func toolByName(tools []asworkspace.Tool, name string) (asworkspace.Tool, error) {
	for _, current := range tools {
		if current.Name() == name {
			return current, nil
		}
	}
	return nil, fmt.Errorf("missing workspace tool %s", name)
}

type csvSchema struct {
	LocalPath   string       `json:"local_path"`
	SandboxPath string       `json:"sandbox_path"`
	RowCount    int          `json:"row_count"`
	Columns     []csvColumn  `json:"columns"`
	SampleRows  []csvRowData `json:"sample_rows"`
}

type csvColumn struct {
	Name         string   `json:"name"`
	InferredType string   `json:"inferred_type"`
	Examples     []string `json:"examples"`
}

type csvRowData map[string]string

func inspectCSV(localPath, displayLocalPath, sandboxPath string) (csvSchema, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return csvSchema{}, fmt.Errorf("open csv fixture: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return csvSchema{}, fmt.Errorf("read csv fixture: %w", err)
	}
	if len(records) < 2 {
		return csvSchema{}, fmt.Errorf("csv fixture must include a header and at least one data row")
	}
	header := records[0]
	rows := records[1:]
	valuesByColumn := make(map[string][]string, len(header))
	for _, row := range rows {
		for index, name := range header {
			if index < len(row) {
				valuesByColumn[name] = append(valuesByColumn[name], row[index])
			}
		}
	}

	columns := make([]csvColumn, 0, len(header))
	for _, name := range header {
		columns = append(columns, csvColumn{
			Name:         name,
			InferredType: inferColumnType(valuesByColumn[name]),
			Examples:     uniqueExamples(valuesByColumn[name], 3),
		})
	}
	sampleLimit := min(len(rows), 5)
	samples := make([]csvRowData, 0, sampleLimit)
	for _, row := range rows[:sampleLimit] {
		sample := csvRowData{}
		for index, name := range header {
			if index < len(row) {
				sample[name] = row[index]
			}
		}
		samples = append(samples, sample)
	}
	return csvSchema{
		LocalPath:   displayLocalPath,
		SandboxPath: sandboxPath,
		RowCount:    len(rows),
		Columns:     columns,
		SampleRows:  samples,
	}, nil
}

func exampleDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve example directory")
	}
	return filepath.Dir(filename), nil
}

func inferColumnType(values []string) string {
	allInt := true
	allFloat := true
	allDate := true
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, err := strconv.Atoi(value); err != nil {
			allInt = false
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			allFloat = false
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			allDate = false
		}
	}
	switch {
	case allInt:
		return "integer"
	case allFloat:
		return "number"
	case allDate:
		return "date"
	default:
		return "string"
	}
}

func uniqueExamples(values []string, limit int) []string {
	seen := map[string]bool{}
	examples := make([]string, 0, limit)
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		examples = append(examples, value)
		if len(examples) == limit {
			break
		}
	}
	sort.Strings(examples)
	return examples
}

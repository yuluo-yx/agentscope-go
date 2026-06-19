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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
	daytonaws "github.com/yuluo-yx/agentscope-go/workspace/daytona"
)

const (
	csvPath    = "/home/daytona/data/sales.csv"
	scriptPath = "/home/daytona/data/analyze_sales.py"
	reportPath = "/home/daytona/data/report.md"
)

func main() {
	ctx := context.Background()
	root := mustTempDir("agentscope-daytona-workspace-example-*")
	defer func() { _ = os.RemoveAll(root) }()

	hostWorkdir := filepath.Join(root, "workspace")
	keepSandbox := getenvBool("AGENTSCOPE_DAYTONA_KEEP_SANDBOX")
	ws := mustWorkspace(daytonaws.NewWorkspace(
		daytonaws.WithImage(getenv("AGENTSCOPE_DAYTONA_IMAGE", "python:3.12")),
		daytonaws.WithHostWorkdir(hostWorkdir),
		daytonaws.WithKeepSandbox(keepSandbox),
		daytonaws.WithRequestTimeout(90*time.Second),
		daytonaws.WithOpenTimeout(4*time.Minute),
	))
	if err := ws.Initialize(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "initialize Daytona workspace failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := ws.Close(context.Background()); err != nil {
			panic(err)
		}
	}()

	tools := mustTools(ws.ListTools(ctx))
	state := asstate.NewAgentState()
	write := findTool(tools, "Write")
	bash := findTool(tools, "Bash")
	read := findTool(tools, "Read")

	runTool(ctx, write, map[string]any{
		"file_path": csvPath,
		"content":   generateSalesCSV(),
	}, state)
	runTool(ctx, write, map[string]any{
		"file_path": scriptPath,
		"content":   analysisScript(),
	}, state)

	analysisResponse := runTool(ctx, bash, map[string]any{
		"command":    "python " + shellQuote(scriptPath),
		"timeout_ms": 60_000,
	}, state)
	if analysisResponse.State != message.ToolResultSuccess {
		panic("analysis failed: " + textContent(analysisResponse.Content))
	}
	var summary analysisSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(textContent(analysisResponse.Content))), &summary); err != nil {
		panic(err)
	}

	reportResponse := runTool(ctx, read, map[string]any{
		"file_path": reportPath,
		"limit":     80,
	}, state)
	if reportResponse.State != message.ToolResultSuccess {
		panic("read report failed: " + textContent(reportResponse.Content))
	}

	fmt.Printf("daytona_workspace_alive=%t sandbox_id=%s keep_sandbox=%t\n", ws.IsAlive(), ws.SandboxID(), keepSandbox)
	fmt.Printf("csv_path=%s report_path=%s\n", csvPath, reportPath)
	fmt.Printf("analysis_total_revenue=%.2f top_region=%s top_product=%s best_day=%s average_discount=%.4f\n",
		summary.TotalRevenue,
		summary.TopRegion,
		summary.TopProduct,
		summary.BestDay,
		summary.AverageDiscount,
	)
	fmt.Println("report_preview:")
	fmt.Println(textContent(reportResponse.Content))
}

type analysisSummary struct {
	TotalRevenue    float64 `json:"total_revenue"`
	TopRegion       string  `json:"top_region"`
	TopProduct      string  `json:"top_product"`
	BestDay         string  `json:"best_day"`
	AverageDiscount float64 `json:"average_discount"`
}

func generateSalesCSV() string {
	regions := []string{"north", "south", "east", "west"}
	products := []string{"notebook", "keyboard", "monitor", "camera"}
	var builder strings.Builder
	builder.WriteString("date,region,product,units,unit_price,discount\n")
	for day := 1; day <= 30; day++ {
		for index, region := range regions {
			product := products[(day+index)%len(products)]
			units := 8 + (day*3+index*5)%37
			price := 29.5 + float64((day+index*7)%12)*6.75
			discount := 0.03 * float64((day+index)%4)
			fmt.Fprintf(&builder, "2026-06-%02d,%s,%s,%d,%.2f,%.2f\n", day, region, product, units, price, discount)
		}
	}
	return builder.String()
}

func analysisScript() string {
	return `import csv
import json
from collections import defaultdict

csv_path = "` + csvPath + `"
report_path = "` + reportPath + `"
analysis_path = "/home/daytona/data/analysis.json"

total_revenue = 0.0
discount_sum = 0.0
row_count = 0
revenue_by_region = defaultdict(float)
units_by_product = defaultdict(int)
revenue_by_day = defaultdict(float)

with open(csv_path, newline="", encoding="utf-8") as source:
    for row in csv.DictReader(source):
        units = int(row["units"])
        unit_price = float(row["unit_price"])
        discount = float(row["discount"])
        revenue = units * unit_price * (1 - discount)
        total_revenue += revenue
        discount_sum += discount
        row_count += 1
        revenue_by_region[row["region"]] += revenue
        units_by_product[row["product"]] += units
        revenue_by_day[row["date"]] += revenue

top_region, top_region_revenue = max(revenue_by_region.items(), key=lambda item: item[1])
top_product, top_product_units = max(units_by_product.items(), key=lambda item: item[1])
best_day, best_day_revenue = max(revenue_by_day.items(), key=lambda item: item[1])
summary = {
    "total_revenue": round(total_revenue, 2),
    "top_region": top_region,
    "top_region_revenue": round(top_region_revenue, 2),
    "top_product": top_product,
    "top_product_units": top_product_units,
    "best_day": best_day,
    "best_day_revenue": round(best_day_revenue, 2),
    "average_discount": round(discount_sum / row_count, 4),
}

with open(analysis_path, "w", encoding="utf-8") as target:
    json.dump(summary, target, indent=2, sort_keys=True)

with open(report_path, "w", encoding="utf-8") as report:
    report.write("# Daytona Sales Analysis\n\n")
    report.write(f"- rows: {row_count}\n")
    report.write(f"- total revenue: {summary['total_revenue']:.2f}\n")
    report.write(f"- top region: {top_region} ({top_region_revenue:.2f})\n")
    report.write(f"- top product: {top_product} ({top_product_units} units)\n")
    report.write(f"- best day: {best_day} ({best_day_revenue:.2f})\n")
    report.write(f"- average discount: {summary['average_discount']:.4f}\n")

print(json.dumps(summary, sort_keys=True))
`
}

func findTool(tools []asworkspace.Tool, name string) asworkspace.Tool {
	for _, current := range tools {
		if current.Name() == name {
			return current
		}
	}
	panic("missing workspace tool: " + name)
}

func runTool(ctx context.Context, current asworkspace.Tool, input map[string]any, state *asstate.AgentState) *tool.ToolResponse {
	chunks, err := current.Execute(ctx, input, state)
	if err != nil {
		panic(err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			panic(err)
		}
	}
	return response
}

func textContent(blocks message.ContentBlockList) string {
	var builder strings.Builder
	for _, block := range blocks {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func getenv(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func getenvBool(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return parsed
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func mustTempDir(pattern string) string {
	dir, err := os.MkdirTemp("", pattern)
	if err != nil {
		panic(err)
	}
	return dir
}

func mustWorkspace(ws *daytonaws.Workspace, err error) *daytonaws.Workspace {
	if err != nil {
		panic(err)
	}
	return ws
}

func mustTools(tools []asworkspace.Tool, err error) []asworkspace.Tool {
	if err != nil {
		panic(err)
	}
	return tools
}

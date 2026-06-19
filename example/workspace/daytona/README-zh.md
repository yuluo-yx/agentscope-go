# Daytona Workspace 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例演示 `workspace/daytona.Workspace`：

- 从 Python 镜像创建 Daytona 沙箱。
- 在 Go 进程中生成 CSV 销售数据集。
- 将 CSV 和 Python 分析脚本写入 Daytona 沙箱。
- 通过 workspace `Bash` 工具在沙箱内执行 Python。
- 通过 workspace `Read` 工具读取生成的 Markdown 报告。

Python 脚本只使用标准库，因此默认 `python:3.12` 镜像即可运行。

## 前置条件

- Go 1.26.4。
- Daytona 账号，或兼容的自托管 Daytona API。
- `DAYTONA_API_KEY`，或 Daytona SDK 支持的 JWT 环境变量组合。

可选变量：

- `DAYTONA_API_URL`：自定义 Daytona API URL。
- `DAYTONA_TARGET`：Daytona target/region。
- `AGENTSCOPE_DAYTONA_IMAGE`：新沙箱使用的镜像，默认 `python:3.12`。
- `AGENTSCOPE_DAYTONA_KEEP_SANDBOX=true`：示例退出后保留沙箱。

## 运行

```bash
cd example/workspace/daytona
DAYTONA_API_KEY=your-key go run .
```

## 预期输出

输出包含：

```text
daytona_workspace_alive=true
csv_path=/home/daytona/data/sales.csv
report_path=/home/daytona/data/report.md
analysis_total_revenue=...
top_region=...
```

默认情况下，示例会在清理阶段删除 Daytona 沙箱。设置 `AGENTSCOPE_DAYTONA_KEEP_SANDBOX=true` 可保留沙箱用于排查。

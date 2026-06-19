# Daytona Workspace 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例演示一个更接近真实使用方式的 Daytona agent 工作流：

- 从 Python 镜像创建 Daytona 沙箱。
- 上传 `data/sales.csv` 中的示例 CSV。
- 将 CSV schema 和用户分析任务交给 Agent。
- 由 Agent 根据 schema 生成 Python 分析代码。
- 通过自定义 `RunPythonAnalysis` 工具把生成的 Python 写入 Daytona 沙箱并执行。
- 最终回答中补充结论、关键证据和数据来源路径。

仓库中的 `python_runner.py` 只是一个执行器包装脚本。实际分析代码由 Agent 运行时生成，并写入 Daytona 沙箱后执行。

## 前置条件

- Go 1.26.4。
- Daytona 账号，或兼容的自托管 Daytona API。
- `DAYTONA_API_KEY`，或 Daytona SDK 支持的 JWT 环境变量组合。
- DashScope 聊天模型密钥：`DASHSCOPE_API_KEY` 或 `AI_DASHSCOPE_API_KEY`。

可选变量：

- `DAYTONA_API_URL`：自定义 Daytona API URL。
- `DAYTONA_TARGET`：Daytona target/region。
- `AGENTSCOPE_DAYTONA_IMAGE`：新沙箱使用的镜像，默认 `python:3.12`。
- `AGENTSCOPE_DAYTONA_KEEP_SANDBOX=true`：示例退出后保留沙箱。
- `DASHSCOPE_MODEL`：DashScope 模型名，默认 `qwen-plus`。

## 运行

```bash
cd example/workspace/daytona
DAYTONA_API_KEY=your-daytona-key DASHSCOPE_API_KEY=your-dashscope-key go run .
```

## 预期输出

输出包含：

```text
daytona_workspace_alive=true sandbox_id=... keep_sandbox=false model=dashscope:qwen-plus
csv_source=data/sales.csv sandbox_csv=/home/daytona/data/sales.csv generated_python=/home/daytona/generated/analysis.py
agent_conclusion:
...
```

默认情况下，示例会在清理阶段删除 Daytona 沙箱。设置 `AGENTSCOPE_DAYTONA_KEEP_SANDBOX=true` 可保留沙箱用于排查。

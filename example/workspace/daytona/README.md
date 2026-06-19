# Daytona Workspace Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example demonstrates `workspace/daytona.Workspace`:

- Create a Daytona sandbox from a Python image.
- Generate a CSV sales dataset in the Go process.
- Write the CSV and a Python analysis script into the Daytona sandbox.
- Execute Python inside the sandbox through the workspace `Bash` tool.
- Read the generated Markdown report through the workspace `Read` tool.

The Python script uses only the standard library, so the default `python:3.12` image is enough.

## Prerequisites

- Go 1.26.4.
- A Daytona account or compatible self-hosted Daytona API.
- `DAYTONA_API_KEY`, or the Daytona JWT environment variables supported by the SDK.

Optional variables:

- `DAYTONA_API_URL`: custom Daytona API URL.
- `DAYTONA_TARGET`: Daytona target/region.
- `AGENTSCOPE_DAYTONA_IMAGE`: image for new sandboxes, default `python:3.12`.
- `AGENTSCOPE_DAYTONA_KEEP_SANDBOX=true`: keep the sandbox after the example exits.

## Run

```bash
cd example/workspace/daytona
DAYTONA_API_KEY=your-key go run .
```

## Expected Output

Output includes:

```text
daytona_workspace_alive=true
csv_path=/home/daytona/data/sales.csv
report_path=/home/daytona/data/report.md
analysis_total_revenue=...
top_region=...
```

By default the example deletes the Daytona sandbox during cleanup. Set `AGENTSCOPE_DAYTONA_KEEP_SANDBOX=true` to keep it for inspection.

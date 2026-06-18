# DashScope Embedding 示例

本示例展示 `embedding/dashscope` 的文本向量调用。代码覆盖 DashScope embedding credential、模型初始化、多输入 embedding、维度配置和输出维度检查。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 模型初始化 | `main()` | 使用 `AI_DASHSCOPE_API_KEY` 创建 `text-embedding-v4` 模型。 |
| 维度配置 | `dashscope.WithDimensions(1024)` | 请求 1,024 维文本向量。 |
| 多输入请求 | `EmbeddingRequest.Inputs` | 一次提交多个文本输入。 |
| 响应检查 | `len(response.Embeddings[0])` | 读取第一条向量维度，确认返回结构。 |
| 结果输出 | `fmt.Printf` | 输出模型名、向量条数和维度。 |

## 运行前提

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

该示例会发起真实 DashScope embedding 请求。未设置 API Key 时，模型创建或调用会失败。

## 快速运行

```bash
cd example/model/dashscope/embedding
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run .
```

输出示例：

```text
dashscope_embedding=ok model=dashscope:text-embedding-v4 embeddings=2 dimensions=1024
```

## 代码功能解读

### 创建文本向量模型

示例创建 `text-embedding-v4` 模型，并指定返回维度：

```go
model, err := dashscope.NewTextModel(
    credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).EmbeddingCredential(),
    "text-embedding-v4",
    dashscope.WithDimensions(1024),
)
```

`EmbeddingCredential()` 会从统一 DashScope credential 中生成 embedding provider 所需的 credential。`WithDimensions(1024)` 表示请求 1,024 维向量。

### 构造多输入请求

请求体使用 `EmbeddingRequest`：

```go
response, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{
    Inputs: []asembedding.EmbeddingInput{
        asembedding.NewTextInput("AgentScope Go makes agent applications easier to compose."),
        asembedding.NewTextInput("Credential adapters keep provider examples consistent."),
    },
})
```

每个 `NewTextInput` 会生成一条文本输入。返回值中的 `Embeddings` 与输入顺序对应。

### 检查输出维度

示例读取第一条向量的长度：

```go
firstDimensions := 0
if len(response.Embeddings) > 0 {
    firstDimensions = len(response.Embeddings[0])
}
```

这能确认服务端返回的向量维度是否符合 `WithDimensions` 的配置。

## 常见问题

### 认证失败

确认 `AI_DASHSCOPE_API_KEY` 是否设置，并确认账号有 embedding 模型调用权限。

### 返回维度不符合预期

先确认模型是否支持自定义维度，再检查 `dashscope.WithDimensions(1024)` 是否被保留在模型初始化参数中。

### 输入文本为空

生产代码应在调用前校验输入内容。空文本可能导致 provider 返回参数错误，或者生成无意义向量。

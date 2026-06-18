# DashScope Embedding Example

This example demonstrates text embeddings through `embedding/dashscope`. It covers DashScope embedding credentials, model construction, batched text inputs, dimension configuration, and response dimension checks.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Model setup | `main()` | Creates `text-embedding-v4` from `AI_DASHSCOPE_API_KEY`. |
| Dimension option | `dashscope.WithDimensions(1024)` | Requests 1,024-dimensional vectors. |
| Batched inputs | `EmbeddingRequest.Inputs` | Sends multiple text inputs in one request. |
| Response check | `len(response.Embeddings[0])` | Reads the first returned vector length. |
| Output | `fmt.Printf` | Prints model name, embedding count, and dimension count. |

## Prerequisites

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

This example makes a real DashScope embedding request. Model construction or calls will fail when the key is missing or invalid.

## Run

```bash
cd example/model/dashscope/embedding
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run .
```

Example output:

```text
dashscope_embedding=ok model=dashscope:text-embedding-v4 embeddings=2 dimensions=1024
```

## Code Walkthrough

### Create the Text Model

The example creates `text-embedding-v4` and requests a fixed dimension:

```go
model, err := dashscope.NewTextModel(
    credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).EmbeddingCredential(),
    "text-embedding-v4",
    dashscope.WithDimensions(1024),
)
```

`EmbeddingCredential()` adapts the shared DashScope credential to the embedding provider. `WithDimensions(1024)` requests 1,024-dimensional vectors.

### Build Batched Inputs

The request uses `EmbeddingRequest`:

```go
response, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{
    Inputs: []asembedding.EmbeddingInput{
        asembedding.NewTextInput("AgentScope Go makes agent applications easier to compose."),
        asembedding.NewTextInput("Credential adapters keep provider examples consistent."),
    },
})
```

Each `NewTextInput` produces one text input. Returned embeddings follow the same order as the inputs.

### Check Returned Dimensions

The example checks the first vector length:

```go
firstDimensions := 0
if len(response.Embeddings) > 0 {
    firstDimensions = len(response.Embeddings[0])
}
```

This confirms whether the service returned the requested vector shape.

## Troubleshooting

### Authentication Error

Check `AI_DASHSCOPE_API_KEY` and confirm that the account can access embedding models.

### Unexpected Dimension Count

Confirm that the model supports custom dimensions, then check that `dashscope.WithDimensions(1024)` is still passed during model construction.

### Empty Input Text

Validate text before calling the provider. Empty text can produce provider parameter errors or low-value vectors.

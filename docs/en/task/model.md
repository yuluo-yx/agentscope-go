# Models

The `model` package defines the provider contract. Provider packages adapt SDK-specific messages, tool schemas, streaming events, and token counting to this contract.

## ChatModel

```go
type ChatModel interface {
	Name() string
	Call(context.Context, CallRequest) (*ChatResponse, error)
	Stream(context.Context, CallRequest) (<-chan ChatResponse, error)
	CountTokens(CallRequest) (int, error)
}
```

## Providers

| Package | Notes |
| --- | --- |
| `model/openai` | OpenAI SDK integration |
| `model/anthropic` | Anthropic SDK integration |
| `model/dashscope` | DashScope OpenAI-compatible endpoint |
| `model/deepseek` | OpenAI-compatible wrapper |
| `model/moonshot` | OpenAI-compatible wrapper |
| `model/xai` | OpenAI-compatible wrapper |
| `model/ollama` | Ollama official Go API |

## Embeddings

The `embedding` package defines embedding requests, responses, cache helpers,
and provider metadata. Python-compatible embedding model cards are embedded in
provider packages that have Python sources:

```go
cards, err := dashscopeembedding.ListModels()
```

Current embedded embedding model cards are copied from Python AgentScope for
`embedding/dashscope`, `embedding/gemini`, and `embedding/openai`.

## Text-to-Speech

The `tts` package defines the text-to-speech provider contract. It uses
`tts.Request` and `tts.Response`; audio is returned as a `message.DataBlock`
whose source is base64 encoded audio data.

DashScope native TTS support is available through `tts/dashscope`:

```go
speech, err := dashscopetts.NewModel(
	dashscopetts.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
	"qwen3-tts-flash",
	dashscopetts.WithStream(false),
)
chunks, err := speech.Synthesize(ctx, tts.Request{Text: "hello"})
for chunk := range chunks {
	if chunk.Content == nil {
		continue
	}
	source := chunk.Content.Source.(*message.Base64Source)
	fmt.Println(source.MediaType, len(source.Data))
}
```

DashScope TTS model cards are also copied from Python AgentScope and exposed by
`dashscopetts.ListModels()`, including both normal and realtime cards.

`WithStream(true)` emits WAV-compatible streaming chunks: the first chunk
contains a streaming WAV header followed by PCM bytes, and later chunks contain
additional PCM bytes under the same `audio/wav` media type. `WithStream(false)`
aggregates provider PCM chunks into one complete WAV payload.

## Tool Schemas

Tools are passed to models as OpenAI-compatible function schemas:

```go
schemas, err := kit.ToolSchemas()
response, err := chat.Call(ctx, model.CallRequest{
	Messages: messages,
	Tools:    schemas,
})
```

`Call` and `Stream` both use `ChatResponse`. Use the content query helpers to read text blocks or tool calls:

```go
text := response.GetTextContent()
if text != nil {
	fmt.Println(*text)
}

toolCalls := response.GetContentBlocks("tool_call")
if len(toolCalls) > 0 {
	fmt.Println(toolCalls[0].BlockID())
}
```

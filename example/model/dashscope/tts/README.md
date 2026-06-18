# DashScope TTS Example

This example demonstrates speech synthesis through `audio/tts/dashscope`. It covers TTS credentials, streaming synthesis, Base64 audio chunk decoding, and local WAV file writing.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Model setup | `main()` | Creates `qwen3-tts-flash` from `AI_DASHSCOPE_API_KEY`. |
| Streaming synthesis | `dashscope.WithStream(true)` | Returns audio content in chunks. |
| Request | `astts.Request` | Sets the text to synthesize. |
| Audio decoding | `base64.StdEncoding.DecodeString` | Decodes returned Base64 audio chunks. |
| File output | `os.Create("output.wav")` | Writes audio bytes into a local file. |

## Prerequisites

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

This example makes a real DashScope TTS request and writes `output.wav` in the current directory.

## Run

```bash
cd example/model/dashscope/tts
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run .
```

Example output:

```text
dashscope_tts=ok model=dashscope:qwen3-tts-flash chunks=4 audio_bytes=123456 file=output.wav
```

## Code Walkthrough

### Create the TTS Model

```go
model, err := dashscope.NewModel(
    credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).TTSCredential(),
    "qwen3-tts-flash",
    dashscope.WithStream(true),
)
```

`TTSCredential()` adapts the shared DashScope credential to the TTS provider. `WithStream(true)` asks the provider to return audio in chunks.

### Start Synthesis

```go
responses, err := model.Synthesize(context.Background(), astts.Request{
    Text: "AgentScope Go text to speech example.",
})
```

`Synthesize` returns a channel. Each response may contain audio content or an error.

### Write the Audio File

The example decodes each Base64 audio chunk and appends it to `output.wav`:

```go
source, ok := response.Content.Source.(*message.Base64Source)
pcm, err := base64.StdEncoding.DecodeString(source.Data)
_, err = f.Write(pcm)
```

The loop counts chunks and total bytes before printing the output path.

## Troubleshooting

### Authentication Error

Check `AI_DASHSCOPE_API_KEY` and confirm that the account can access TTS models.

### No File Created

Check write permissions in the current directory and inspect `response.Error` values from the stream.

### Audio Cannot Be Played

Confirm that the returned audio format matches the file extension. The example writes `output.wav`; the actual format is determined by provider output.

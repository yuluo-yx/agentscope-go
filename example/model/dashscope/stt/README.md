# DashScope STT Example

This example demonstrates speech recognition through `audio/stt/dashscope`. It uses batch `paraformer-v2` by default and can run Qwen-ASR realtime recognition sessions with `--mode realtime`.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Argument check | `flag.NArg() < 1` | Requires a local audio file path. |
| Audio reading | `os.ReadFile` | Reads local audio file bytes. |
| Model setup | `dashscope.NewModel` | Creates `paraformer-v2` from `AI_DASHSCOPE_API_KEY`. |
| Request construction | `stt.NewAudioBlock` | Wraps raw bytes as an `audio/wav` input block. |
| Recognition call | `model.Recognize` | Starts recognition and returns a response channel. |
| Realtime recognition | `dashscope.NewRealtimeModel` | Creates `qwen3-asr-flash-realtime` and pushes audio through a `Session`. |
| Output | `response.Text` | Prints recognized text and language. |

## Prerequisites

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

For batch mode, prepare a WAV file such as `sample.wav`. For realtime mode, prepare 16 kHz PCM data such as `sample.pcm`. This example makes a real DashScope STT request.

## Run

```bash
cd example/model/dashscope/stt
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run . ./sample.wav
```

Realtime recognition:

```bash
go run . --mode realtime --language zh ./sample.pcm
```

Example output:

```text
dashscope_stt=ok model=dashscope:paraformer-v2 text="hello" language=zh
dashscope_stt_realtime=partial model=dashscope:qwen3-asr-flash-realtime text="你" language=zh
dashscope_stt_realtime=final model=dashscope:qwen3-asr-flash-realtime text="你好" language=zh
```

## Code Walkthrough

### Read the Audio Path

```go
if flag.NArg() < 1 {
    panic("usage: go run . [--mode batch|realtime] ./audio.wav")
}
rawAudio, err := os.ReadFile(flag.Arg(0))
```

The example requires the caller to pass an audio path so test audio does not need to live in the repository.

### Create the STT Model

```go
model, err := dashscope.NewModel(
    credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).STTCredential(),
    "paraformer-v2",
)
```

`STTCredential()` adapts the shared DashScope credential to the speech-to-text provider.

### Build the Recognition Request

```go
responses, err := model.Recognize(context.Background(), stt.Request{
    Audio: stt.NewAudioBlock(rawAudio, "audio/wav"),
})
```

`NewAudioBlock` stores the raw file bytes plus MIME type. The example uses `audio/wav`, so the input file should match that format.

### Read Recognition Results

```go
for response := range responses {
    if response.Error != nil {
        panic(response.Error)
    }
    if response.Text != "" {
        fmt.Printf("dashscope_stt=ok model=%s text=%q language=%s\n", model.Name(), response.Text, response.Language)
    }
}
```

The provider returns responses through a channel. Check errors and skip empty text blocks.

### Use a Realtime Session

```go
model, err := dashscope.NewRealtimeModel(
    credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).STTCredential(),
    "qwen3-asr-flash-realtime",
    dashscope.WithRealtimeParameters(dashscope.RealtimeParameters{Language: language}),
)
session, err := model.NewSession(ctx, stt.SessionRequest{})
defer session.Close(context.WithoutCancel(ctx))

err = session.Push(ctx, stt.NewAudioBlock(rawAudio, "audio/pcm"))
err = session.Finish(ctx)
for response := range session.Responses() {
    // response.IsLast=false means partial text; true means final text or a terminal error.
}
```

Realtime mode uses server-side VAD by default. `input_audio_buffer.append` has no server acknowledgment, so `Push` only means the audio chunk was written to the WebSocket; recognition failures arrive through `Responses()`.

## Troubleshooting

### Missing Audio Argument

The command must include a file path:

```bash
go run . ./audio.wav
```

### File Read Failed

Check that the path exists and that the current user can read it.

### Empty Recognition Result

For batch mode, confirm that the audio format matches `audio/wav`. For realtime mode, confirm that the input is `audio/pcm` or provider-supported Opus data, and that the file contains clear speech.

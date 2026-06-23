# DashScope STT Example

This example demonstrates speech recognition through `audio/stt/dashscope`. It uses batch `paraformer-v2` by default and can run Qwen-ASR realtime recognition sessions with `--mode realtime`.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Argument check | `flag.NArg() < 1` | Requires a public audio URL for batch mode or a local PCM path for realtime mode. |
| Batch audio input | `message.NewURLSource` | Sends a public HTTP(S) audio URL to DashScope batch recognition. |
| Realtime audio reading | `os.ReadFile` | Reads local PCM audio bytes for the realtime WebSocket session. |
| Model setup | `dashscope.NewModel` | Creates `paraformer-v2` from `AI_DASHSCOPE_API_KEY`. |
| Request construction | `message.NewDataBlock` | Wraps the URL source as an input block for batch recognition. |
| Recognition call | `model.Recognize` | Starts recognition and returns a response channel. |
| Realtime recognition | `dashscope.NewRealtimeModel` | Creates `qwen3-asr-flash-realtime` and pushes audio through a `Session`. |
| Output | `response.Text` | Prints recognized text and language. |

## Prerequisites

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

For batch mode, provide a publicly reachable HTTP(S) audio URL. For realtime mode, prepare 16 kHz PCM data such as `sample.pcm`. This example makes a real DashScope STT request.

## Run

```bash
cd example/model/dashscope/stt
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run . https://dashscope.oss-cn-beijing.aliyuncs.com/samples/audio/paraformer/hello_world_female2.wav
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
    panic("usage: go run . [--mode batch|realtime] <audio-url-or-local-pcm>")
}
```

Batch mode requires a public audio URL because DashScope recorded-file recognition fetches audio from `file_urls`. Realtime mode still reads local PCM bytes before pushing them to the WebSocket session.

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
    Audio: message.NewDataBlock(message.NewURLSource(audioURL, "audio/wav")),
})
```

`NewURLSource` stores the public audio URL plus MIME type. The example uses `audio/wav`, so the referenced audio should match that format.

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

The command must include an audio URL for batch mode or a local PCM path for realtime mode:

```bash
go run . https://dashscope.oss-cn-beijing.aliyuncs.com/samples/audio/paraformer/hello_world_female2.wav
```

### URL Is Not Reachable

For batch mode, make sure the audio URL is reachable by DashScope. Local file paths are only supported by realtime mode.

### Empty Recognition Result

For batch mode, confirm that the URL returns provider-supported audio and that the MIME type matches the file. For realtime mode, confirm that the input is `audio/pcm` or provider-supported Opus data, and that the file contains clear speech.

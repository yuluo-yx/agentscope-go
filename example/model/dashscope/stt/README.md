# DashScope STT Example

This example demonstrates speech recognition through `audio/stt/dashscope`. It covers STT credentials, local audio reading, audio-block construction, recognition requests, and text result output.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Argument check | `len(os.Args) < 2` | Requires a local audio file path. |
| Audio reading | `os.ReadFile` | Reads WAV file bytes. |
| Model setup | `dashscope.NewModel` | Creates `paraformer-v2` from `AI_DASHSCOPE_API_KEY`. |
| Request construction | `stt.NewAudioBlock` | Wraps raw bytes as an `audio/wav` input block. |
| Recognition call | `model.Recognize` | Starts recognition and returns a response channel. |
| Output | `response.Text` | Prints recognized text and language. |

## Prerequisites

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

Prepare a WAV file, for example `sample.wav`. This example makes a real DashScope STT request.

## Run

```bash
cd example/model/dashscope/stt
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run . ./sample.wav
```

Example output:

```text
dashscope_stt=ok model=dashscope:paraformer-v2 text="hello" language=zh
```

## Code Walkthrough

### Read the Audio Path

```go
if len(os.Args) < 2 {
    panic("usage: go run . ./audio.wav")
}
rawAudio, err := os.ReadFile(os.Args[1])
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

## Troubleshooting

### Missing Audio Argument

The command must include a file path:

```bash
go run . ./audio.wav
```

### File Read Failed

Check that the path exists and that the current user can read it.

### Empty Recognition Result

Confirm that the audio format matches `audio/wav` and that the file contains clear speech.

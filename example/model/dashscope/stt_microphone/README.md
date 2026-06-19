# DashScope Microphone Realtime STT Example

This example connects the default local microphone to the `audio/stt/dashscope` Qwen-ASR realtime `Session`. After startup it listens to microphone input and prints realtime transcripts to the console.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Microphone capture | `startMicrophone` | Opens the default input device with `github.com/gen2brain/malgo`. |
| Audio format | `malgo.FormatS16` | Captures 16-bit signed little-endian PCM, mono. |
| Non-blocking callback | `DeviceCallbacks.Data` | Copies audio into a bounded queue without blocking the audio thread. |
| Realtime streaming | `startAudioSender` | Pushes queued chunks with `session.Push`. |
| Transcript output | `startResponsePrinter` | Reads partial/final text from `session.Responses()`. |
| Graceful shutdown | `Ctrl+C` | Stops the microphone, flushes queued audio, calls `session.Finish`, and reads final results. |

## Prerequisites

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

This example makes a real DashScope WebSocket request and accesses your local microphone. On macOS, the first run usually prompts for microphone permission for your terminal or IDE.

`malgo` uses CGO and wraps miniaudio. macOS usually builds directly; Linux may need audio backend development packages such as ALSA or PulseAudio; Windows needs a local C compiler.

## Run

```bash
cd example/model/dashscope/stt_microphone
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run .
```

Speak into the microphone. The console refreshes partial text and prints final text after server-side VAD detects an utterance boundary:

```text
listening language=zh sample_rate=16000 chunk_ms=100; press Ctrl+C to stop
partial: ni
final: ni hao
```

Press `Ctrl+C` to stop. The program stops the microphone first, calls `session.Finish`, and then drains final responses.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--language` | `zh` | Language hint passed to Qwen-ASR realtime. |
| `--sample-rate` | `16000` | Microphone sample rate. Supported values are `8000` and `16000`. |
| `--chunk-ms` | `100` | Target audio chunk duration in milliseconds. |
| `--vad-threshold` | `0` | Server VAD threshold, from `-1` to `1`. |
| `--silence-ms` | `400` | Server VAD silence duration, from `200` to `6000` ms. |
| `--queue-size` | `32` | Bounded queue length between microphone callback and network sender. |

Example:

```bash
go run . --language zh --sample-rate 16000 --chunk-ms 100 --silence-ms 600
```

## Code Walkthrough

### Create the Realtime Model

```go
model, err := dashscope.NewRealtimeModel(
    credential.NewDashScope(cfg.apiKey).STTCredential(),
    "qwen3-asr-flash-realtime",
    dashscope.WithRealtimeParameters(dashscope.RealtimeParameters{
        Language:           cfg.language,
        SampleRate:         cfg.sampleRate,
        Mode:               dashscope.RealtimeModeVAD,
        VADThreshold:       cfg.vadThreshold,
        VADSilenceDuration: time.Duration(cfg.silenceMS) * time.Millisecond,
    }),
)
```

The example uses server-side VAD so DashScope decides utterance boundaries automatically. Final text arrives with `response.IsLast=true`.

### Open the Default Microphone

```go
deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
deviceConfig.Capture.Format = malgo.FormatS16
deviceConfig.Capture.Channels = 1
deviceConfig.SampleRate = uint32(cfg.sampleRate)
deviceConfig.PeriodSizeInFrames = uint32(cfg.sampleRate * cfg.chunkMS / 1000)
```

The capture format matches the realtime parameters: PCM, mono, `8000` or `16000` Hz.

### Keep the Audio Callback Non-Blocking

```go
callbacks := malgo.DeviceCallbacks{
    Data: func(_, inputSamples []byte, _ uint32) {
        chunk := append([]byte(nil), inputSamples...)
        select {
        case audioChunks <- chunk:
        default:
        }
    },
}
```

The audio callback should not touch the network. It only copies the current audio buffer and tries to enqueue it. If the network side falls behind and the queue is full, the current chunk is dropped to protect microphone capture.

### Push Audio and Read Responses

```go
for chunk := range audioChunks {
    err := session.Push(ctx, stt.NewAudioBlock(chunk, "audio/pcm"))
}

for response := range session.Responses() {
    line, ok := formatResponseLine(response)
}
```

`Push` only means the audio chunk was written to the WebSocket. Recognition text, final chunks, and asynchronous errors come from `Responses()`.

## Troubleshooting

### `AI_DASHSCOPE_API_KEY is required`

Set your DashScope API key first:

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

### Microphone Open Failed

Check that your terminal or IDE has microphone permission. On macOS, see System Settings -> Privacy & Security -> Microphone.

### No Transcript Text

Check microphone input volume, use `16000` Hz sample rate, and speak clear short utterances in a quiet environment. Server-side VAD emits final text after it detects silence.

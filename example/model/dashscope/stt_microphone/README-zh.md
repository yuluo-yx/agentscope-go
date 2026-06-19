# DashScope 麦克风实时 STT 示例

本示例展示如何把本机默认麦克风输入接入 `audio/stt/dashscope` 的 Qwen-ASR realtime `Session`，程序启动后持续监听麦克风，并把实时识别结果输出到控制台。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 麦克风采集 | `startMicrophone` | 使用 `github.com/gen2brain/malgo` 打开默认输入设备。 |
| 音频格式 | `malgo.FormatS16` | 采集 16-bit signed little-endian PCM，单声道。 |
| 非阻塞回调 | `DeviceCallbacks.Data` | 回调只复制音频并写入有界队列，避免阻塞音频线程。 |
| 实时推流 | `startAudioSender` | 后台 goroutine 从队列读取音频块并调用 `session.Push`。 |
| 结果输出 | `startResponsePrinter` | 从 `session.Responses()` 读取 partial/final 文本并打印。 |
| 优雅退出 | `Ctrl+C` | 停止麦克风，推完队列内音频，调用 `session.Finish`，再读取最终结果。 |

## 运行前提

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

本示例会发起真实 DashScope WebSocket 请求，并访问本机麦克风。macOS 首次运行时通常会弹出麦克风权限提示，需要允许当前终端或 IDE 访问麦克风。

`malgo` 使用 CGO 包装 miniaudio。macOS 通常可直接编译；Linux 可能需要系统音频后端相关开发包，例如 ALSA 或 PulseAudio；Windows 需要本机 C 编译环境。

## 快速运行

```bash
cd example/model/dashscope/stt_microphone
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run .
```

对着麦克风说话，控制台会持续刷新中间结果，并在服务端 VAD 判断一句话结束后输出最终文本：

```text
$ go run . --language zh --sample-rate 16000 --chunk-ms 100 --silence-ms 600
listening language=zh sample_rate=16000 chunk_ms=100; press Ctrl+C to stop
final: 哈喽，AI框架。。
^C
stopping microphone and finishing session
```

按 `Ctrl+C` 结束程序。程序会先停止麦克风，再调用 `session.Finish` 获取收尾结果。

## 参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--language` | `zh` | 传给 Qwen-ASR realtime 的语言提示。 |
| `--sample-rate` | `16000` | 麦克风采样率，只支持 `8000` 或 `16000`。 |
| `--chunk-ms` | `100` | 每个音频块目标时长，单位毫秒。 |
| `--vad-threshold` | `0` | 服务端 VAD 阈值，范围 `-1` 到 `1`。 |
| `--silence-ms` | `400` | 服务端 VAD 静音结束时长，范围 `200` 到 `6000`。 |
| `--queue-size` | `32` | 麦克风回调到网络发送之间的有界队列长度。 |

示例：

```bash
go run . --language zh --sample-rate 16000 --chunk-ms 100 --silence-ms 600
```

## 代码功能解读

### 创建实时模型

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

示例固定使用服务端 VAD，让 DashScope 自动判断一句话何时结束。每段最终文本会以 `response.IsLast=true` 返回。

### 打开默认麦克风

```go
deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
deviceConfig.Capture.Format = malgo.FormatS16
deviceConfig.Capture.Channels = 1
deviceConfig.SampleRate = uint32(cfg.sampleRate)
deviceConfig.PeriodSizeInFrames = uint32(cfg.sampleRate * cfg.chunkMS / 1000)
```

采集格式与 realtime 参数保持一致：PCM、单声道、`8000` 或 `16000` Hz。

### 避免阻塞音频回调

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

音频回调不能直接访问网络。示例只复制当前音频块并尝试写入有界队列；如果网络侧处理不过来，队列满时会丢弃当前块，以保护麦克风采集线程。

### 推送音频并读取结果

```go
for chunk := range audioChunks {
    err := session.Push(ctx, stt.NewAudioBlock(chunk, "audio/pcm"))
}

for response := range session.Responses() {
    line, ok := formatResponseLine(response)
}
```

`Push` 只表示音频块已经写入 WebSocket。识别文本、最终结果和异步错误都通过 `Responses()` 返回。

## 常见问题

### 提示 `AI_DASHSCOPE_API_KEY is required`

先设置 DashScope API Key：

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

### 麦克风无法打开

确认系统已授权当前终端或 IDE 访问麦克风。macOS 可在“系统设置 -> 隐私与安全性 -> 麦克风”中检查授权。

### 没有识别文本

确认麦克风有输入音量，采样率使用 `16000`，并尽量在安静环境下说清晰的短句。服务端 VAD 在检测到静音后才会输出最终文本。

# DashScope STT 示例

本示例展示 `audio/stt/dashscope` 的语音识别调用。默认使用批量 `paraformer-v2`，也可以通过 `--mode realtime` 使用 Qwen-ASR 实时语音识别 Session。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 参数检查 | `flag.NArg() < 1` | 要求命令行传入本地音频文件路径。 |
| 音频读取 | `os.ReadFile` | 读取本地音频文件字节。 |
| 模型初始化 | `dashscope.NewModel` | 使用 `AI_DASHSCOPE_API_KEY` 创建 `paraformer-v2`。 |
| 请求构造 | `stt.NewAudioBlock` | 把原始音频字节包装成 `audio/wav` 输入块。 |
| 识别调用 | `model.Recognize` | 发起语音识别请求并返回响应 channel。 |
| 实时识别 | `dashscope.NewRealtimeModel` | 创建 `qwen3-asr-flash-realtime` 并通过 `Session` 推送音频。 |
| 结果输出 | `response.Text` | 输出识别文本和语言。 |

## 运行前提

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

批量模式需要准备一个 WAV 文件，例如 `sample.wav`。实时模式建议准备 16kHz PCM 文件，例如 `sample.pcm`。示例会发起真实 DashScope STT 请求。

## 快速运行

```bash
cd example/model/dashscope/stt
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run . ./sample.wav
```

实时识别：

```bash
go run . --mode realtime --language zh ./sample.pcm
```

输出示例：

```text
dashscope_stt=ok model=dashscope:paraformer-v2 text="hello" language=zh
dashscope_stt_realtime=partial model=dashscope:qwen3-asr-flash-realtime text="你" language=zh
dashscope_stt_realtime=final model=dashscope:qwen3-asr-flash-realtime text="你好" language=zh
```

## 代码功能解读

### 读取命令行音频路径

```go
if flag.NArg() < 1 {
    panic("usage: go run . [--mode batch|realtime] ./audio.wav")
}
rawAudio, err := os.ReadFile(flag.Arg(0))
```

示例要求调用方显式传入音频文件路径。这样可以避免把测试音频写死在仓库中。

### 创建 STT 模型

```go
model, err := dashscope.NewModel(
    credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).STTCredential(),
    "paraformer-v2",
)
```

`STTCredential()` 从统一 DashScope credential 中生成语音识别 provider 所需的 credential。

### 构造识别请求

```go
responses, err := model.Recognize(context.Background(), stt.Request{
    Audio: stt.NewAudioBlock(rawAudio, "audio/wav"),
})
```

`NewAudioBlock` 会把文件字节和 MIME 类型一起放进请求。示例固定使用 `audio/wav`，因此传入文件应与该格式一致。

### 读取识别结果

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

响应通过 channel 返回。循环中既要检查错误，也要过滤空文本块。

### 使用实时 Session

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
    // response.IsLast=false 表示实时中间结果，true 表示最终文本或终止错误。
}
```

实时模式默认使用服务端 VAD。`input_audio_buffer.append` 没有服务端确认，因此 `Push` 只表示音频块已经写入 WebSocket；识别失败会通过 `Responses()` 返回。

## 常见问题

### 缺少音频参数

命令必须带文件路径：

```bash
go run . ./audio.wav
```

### 文件读取失败

确认路径存在，并确认当前用户有读取权限。

### 识别结果为空

批量模式确认音频格式与 `audio/wav` 匹配；实时模式确认输入是 `audio/pcm` 或服务端支持的 Opus 数据，并确认文件中包含清晰语音。

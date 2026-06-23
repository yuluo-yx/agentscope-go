# DashScope STT 示例

本示例展示 `audio/stt/dashscope` 的语音识别调用。默认使用批量 `paraformer-v2`，也可以通过 `--mode realtime` 使用 Qwen-ASR 实时语音识别 Session。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 参数检查 | `flag.NArg() < 1` | 批量模式要求传入公网音频 URL，实时模式要求传入本地 PCM 路径。 |
| 批量音频输入 | `message.NewURLSource` | 把公网 HTTP(S) 音频 URL 传给 DashScope 批量识别。 |
| 实时音频读取 | `os.ReadFile` | 为实时 WebSocket Session 读取本地 PCM 音频字节。 |
| 模型初始化 | `dashscope.NewModel` | 使用 `AI_DASHSCOPE_API_KEY` 创建 `paraformer-v2`。 |
| 请求构造 | `message.NewDataBlock` | 把 URL source 包装成批量识别输入块。 |
| 识别调用 | `model.Recognize` | 发起语音识别请求并返回响应 channel。 |
| 实时识别 | `dashscope.NewRealtimeModel` | 创建 `qwen3-asr-flash-realtime` 并通过 `Session` 推送音频。 |
| 结果输出 | `response.Text` | 输出识别文本和语言。 |

## 运行前提

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

批量模式需要传入 DashScope 可访问的公网 HTTP(S) 音频 URL。实时模式建议准备 16kHz PCM 文件，例如 `sample.pcm`。示例会发起真实 DashScope STT 请求。

## 快速运行

```bash
cd example/model/dashscope/stt
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run . https://dashscope.oss-cn-beijing.aliyuncs.com/samples/audio/paraformer/hello_world_female2.wav
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
    panic("usage: go run . [--mode batch|realtime] <audio-url-or-local-pcm>")
}
```

批量模式需要公网音频 URL，因为 DashScope 录音文件识别会从 `file_urls` 拉取音频。实时模式仍然读取本地 PCM 字节，再通过 WebSocket Session 推送。

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
    Audio: message.NewDataBlock(message.NewURLSource(audioURL, "audio/wav")),
})
```

`NewURLSource` 会把公网音频 URL 和 MIME 类型一起放进请求。示例固定使用 `audio/wav`，因此 URL 指向的音频应与该格式一致。

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

批量模式命令必须带音频 URL，实时模式命令必须带本地 PCM 路径：

```bash
go run . https://dashscope.oss-cn-beijing.aliyuncs.com/samples/audio/paraformer/hello_world_female2.wav
```

### URL 不可访问

批量模式需要确认音频 URL 可被 DashScope 访问。本地文件路径只适用于实时模式。

### 识别结果为空

批量模式确认 URL 返回服务端支持的音频，且 MIME 类型和文件格式一致；实时模式确认输入是 `audio/pcm` 或服务端支持的 Opus 数据，并确认文件中包含清晰语音。

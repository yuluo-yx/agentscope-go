# DashScope TTS 示例

本示例展示 `audio/tts/dashscope` 的语音合成调用。代码覆盖 TTS credential、流式语音合成、Base64 音频块解码和本地 WAV 文件写入。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 模型初始化 | `main()` | 使用 `AI_DASHSCOPE_API_KEY` 创建 `qwen3-tts-flash`。 |
| 流式合成 | `dashscope.WithStream(true)` | 让模型按块返回音频内容。 |
| 请求构造 | `astts.Request` | 设置待合成文本。 |
| 音频解码 | `base64.StdEncoding.DecodeString` | 把返回的 Base64 音频块解码成字节。 |
| 文件写入 | `os.Create("output.wav")` | 把音频块顺序写入本地文件。 |

## 运行前提

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

该示例会发起真实 DashScope TTS 请求，并在当前目录生成 `output.wav`。

## 快速运行

```bash
cd example/model/dashscope/tts
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run .
```

输出示例：

```text
dashscope_tts=ok model=dashscope:qwen3-tts-flash chunks=4 audio_bytes=123456 file=output.wav
```

## 代码功能解读

### 创建 TTS 模型

```go
model, err := dashscope.NewModel(
    credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).TTSCredential(),
    "qwen3-tts-flash",
    dashscope.WithStream(true),
)
```

`TTSCredential()` 从统一 DashScope credential 中生成 TTS provider 所需的 credential。`WithStream(true)` 表示服务端会按块返回音频内容。

### 发起合成请求

```go
responses, err := model.Synthesize(context.Background(), astts.Request{
    Text: "AgentScope Go text to speech example.",
})
```

`Synthesize` 返回一个 channel。每个响应块可能包含音频内容，也可能包含错误。

### 写入音频文件

示例把每个 Base64 音频块解码后写入 `output.wav`：

```go
source, ok := response.Content.Source.(*message.Base64Source)
pcm, err := base64.StdEncoding.DecodeString(source.Data)
_, err = f.Write(pcm)
```

循环会统计音频块数量和总字节数，最后打印输出文件路径。

## 常见问题

### 认证失败

确认 `AI_DASHSCOPE_API_KEY` 是否设置，并确认账号有 TTS 模型调用权限。

### 没有生成文件

确认进程对当前目录有写权限，并检查是否在流式响应中收到了 `response.Error`。

### 音频无法播放

确认返回音频格式与文件扩展名一致。示例写入 `output.wav`，实际格式以 provider 返回内容为准。

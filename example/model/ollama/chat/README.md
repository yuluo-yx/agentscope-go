# Ollama

```yml
services:
   ollama:
     # tips: need update
     volumes:
       - /Users/shown/workspace/ai/models:/root/.ollama
     container_name: ollama
     tty: true
     restart: unless-stopped
     image: ollama/ollama:latest
     ports:
       - 11434:11434
```

```shell
# test ollama server

#!/bin/zsh

curl http://localhost:11434/api/chat -d '{
  "model": "llama3.1",
  "messages": [
    {
      "role": "user",
      "content": "why is the sky blue?"
    }
  ],
  "stream": true
}'
```

## Demo

```shell

step 1: go mod tidy

step 2: go run .

output:

$ go run .
start chat call: ------------------
chat_model=ollama:llama3.1 ollama_model=ollama:llama3.1 tools=1 multimodal_blocks=2 estimated_tokens=71
ollama_live=ok response="AgentScope Go is a remote desktop access and monitoring tool for Windows systems."
ollama_weather=ok tool=GetWeather input={"city":"张区"} response="杭州的天气很好，阳光明媚。"

start stream chat call: ------------------
chat_model=ollama:llama3.1 ollama_model=ollama:llama3.1 tools=1 multimodal_blocks=2 estimated_tokens=71
ollama_stream_delta="Agent"
ollama_stream_delta="Scope"
ollama_stream_delta=" Go"
ollama_stream_delta=" is"
ollama_stream_delta=" a"
ollama_stream_delta=" remote"
ollama_stream_delta=" desktop"
ollama_stream_delta=" access"
ollama_stream_delta=" and"
ollama_stream_delta=" monitoring"
ollama_stream_delta=" tool"
ollama_stream_delta=" for"
ollama_stream_delta=" Windows"
ollama_stream_delta=" systems"
ollama_stream_delta="."
ollama_stream=ok response="AgentScope Go is a remote desktop access and monitoring tool for Windows systems."


```

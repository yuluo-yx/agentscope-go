// Copyright The AgentScope Go Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	astts "github.com/yuluo-yx/agentscope-go/tts"
	"github.com/yuluo-yx/agentscope-go/tts/dashscope"
)

func main() {
	model, err := dashscope.NewModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).TTSCredential(),
		"qwen3-tts-flash",
		dashscope.WithStream(true),
	)
	if err != nil {
		panic(err)
	}

	responses, err := model.Synthesize(context.Background(), astts.Request{
		Text: "AgentScope Go text to speech example.",
	})
	if err != nil {
		panic(err)
	}

	chunks := 0
	base64Bytes := 0
	for response := range responses {
		if response.Error != nil {
			panic(response.Error)
		}
		if response.Content == nil {
			continue
		}
		source, ok := response.Content.Source.(*message.Base64Source)
		if !ok {
			continue
		}
		chunks++
		base64Bytes += len(source.Data)
	}

	fmt.Printf("dashscope_tts=ok model=%s chunks=%d base64_bytes=%d\n", model.Name(), chunks, base64Bytes)
}

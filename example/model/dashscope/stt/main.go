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

	"github.com/yuluo-yx/agentscope-go/audio/stt"
	"github.com/yuluo-yx/agentscope-go/audio/stt/dashscope"
	"github.com/yuluo-yx/agentscope-go/credential"
)

func main() {
	if len(os.Args) < 2 {
		panic("usage: go run . ./audio.wav")
	}
	rawAudio, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}

	model, err := dashscope.NewModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).STTCredential(),
		"paraformer-v2",
	)
	if err != nil {
		panic(err)
	}

	responses, err := model.Recognize(context.Background(), stt.Request{
		Audio: stt.NewAudioBlock(rawAudio, "audio/wav"),
	})
	if err != nil {
		panic(err)
	}

	for response := range responses {
		if response.Error != nil {
			panic(response.Error)
		}
		if response.Text != "" {
			fmt.Printf("dashscope_stt=ok model=%s text=%q language=%s\n", model.Name(), response.Text, response.Language)
		}
	}
}

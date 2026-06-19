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
	"flag"
	"fmt"
	"os"

	"github.com/yuluo-yx/agentscope-go/audio/stt"
	"github.com/yuluo-yx/agentscope-go/audio/stt/dashscope"
	"github.com/yuluo-yx/agentscope-go/credential"
)

func main() {
	mode := flag.String("mode", "batch", "recognition mode: batch or realtime")
	language := flag.String("language", "zh", "language hint for realtime recognition")
	flag.Parse()
	if flag.NArg() < 1 {
		panic("usage: go run . [--mode batch|realtime] ./audio.wav")
	}
	rawAudio, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	switch *mode {
	case "batch":
		if err := runBatch(ctx, rawAudio); err != nil {
			panic(err)
		}
	case "realtime":
		if err := runRealtime(ctx, rawAudio, *language); err != nil {
			panic(err)
		}
	default:
		panic("mode must be batch or realtime")
	}
}

func runBatch(ctx context.Context, rawAudio []byte) error {
	model, err := dashscope.NewModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).STTCredential(),
		"paraformer-v2",
	)
	if err != nil {
		return err
	}

	responses, err := model.Recognize(ctx, stt.Request{
		Audio: stt.NewAudioBlock(rawAudio, "audio/wav"),
	})
	if err != nil {
		return err
	}

	for response := range responses {
		if response.Error != nil {
			return response.Error
		}
		if response.Text != "" {
			fmt.Printf("dashscope_stt=ok model=%s text=%q language=%s\n", model.Name(), response.Text, response.Language)
		}
	}
	return nil
}

func runRealtime(ctx context.Context, rawAudio []byte, language string) error {
	model, err := dashscope.NewRealtimeModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).STTCredential(),
		"qwen3-asr-flash-realtime",
		dashscope.WithRealtimeParameters(dashscope.RealtimeParameters{Language: language}),
	)
	if err != nil {
		return err
	}
	session, err := model.NewSession(ctx, stt.SessionRequest{})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close(context.WithoutCancel(ctx)) }()

	if err := session.Push(ctx, stt.NewAudioBlock(rawAudio, "audio/pcm")); err != nil {
		return err
	}
	if err := session.Finish(ctx); err != nil {
		return err
	}
	for response := range session.Responses() {
		if response.Error != nil {
			return response.Error
		}
		if response.Text == "" {
			continue
		}
		status := "partial"
		if response.IsLast {
			status = "final"
		}
		fmt.Printf("dashscope_stt_realtime=%s model=%s text=%q language=%s\n", status, model.Name(), response.Text, response.Language)
	}
	return nil
}

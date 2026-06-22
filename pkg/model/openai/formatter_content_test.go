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

package openai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
)

func TestFormatMessagesCoversHintsToolsAndProviderSpecificData(t *testing.T) {
	t.Parallel()

	msgs := []*message.Message{
		{
			Role: message.RoleUser,
			Name: "user",
			Content: message.ContentBlockList{
				message.NewTextBlock("look"),
				message.NewHintBlock(message.ContentBlockList{
					message.NewTextBlock("hint"),
					message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
				}),
				message.NewDataBlock(message.NewURLSource("https://example.com/video.mp4", "video/mp4")),
			},
		},
		{
			Role: message.RoleAssistant,
			Content: message.ContentBlockList{
				message.NewTextBlock("calling"),
				message.NewToolCallBlock("call-1", "Search", `{"q":"go"}`),
				message.NewThinkingBlock("hidden"),
			},
		},
		{
			Role: message.RoleAssistant,
			Content: message.ContentBlockList{
				message.NewTextBlock("result follows"),
				message.NewToolResultBlock("call-1", "Search", message.ToolResultOutput{Raw: "ok"}, message.ToolResultSuccess),
			},
		},
	}
	formatted, err := formatMessages(msgs, "dashscope")
	if err != nil {
		t.Fatalf("formatMessages returned error: %v", err)
	}
	if len(formatted) != 4 {
		t.Fatalf("tool result should split assistant and tool messages, got %d", len(formatted))
	}

	if _, err := formatMessages([]*message.Message{{Role: "bad", Content: message.ContentBlockList{message.NewTextBlock("x")}}}); err == nil {
		t.Fatalf("unsupported message role should fail")
	}
	if _, _, _, err := splitContent(message.ContentBlockList{nil}, "openai"); err == nil {
		t.Fatalf("unsupported content block should fail")
	}
	if _, err := hintContentParts(message.NewHintBlock(message.ContentBlockList{message.NewThinkingBlock("bad")}), "openai"); err == nil {
		t.Fatalf("unsupported nested hint block should fail")
	}
}

func TestDataBlockPartProviderBranches(t *testing.T) {
	t.Parallel()

	video, err := dataBlockPart(message.NewDataBlock(message.NewBase64Source("AAAA", "video/mp4")), "dashscope")
	if err != nil {
		t.Fatalf("DashScope video part returned error: %v", err)
	}
	encoded, err := json.Marshal(video)
	if err != nil {
		t.Fatalf("marshal video part: %v", err)
	}
	if !strings.Contains(string(encoded), "video_url") {
		t.Fatalf("DashScope video should use raw video_url part: %s", encoded)
	}

	audio, err := dataBlockPart(message.NewDataBlock(message.NewURLSource("https://example.com/audio.wav", "audio/wav")), "dashscope")
	if err != nil || audio == nil {
		t.Fatalf("DashScope URL audio should be accepted: part=%#v err=%v", audio, err)
	}
	if _, err := dataBlockPart(message.NewDataBlock(message.NewBase64Source("AAAA", "video/mp4")), "openai"); !isCapabilityError(err, asmodel.ModelCapabilityVideo) {
		t.Fatalf("OpenAI base64 video should return capability error, got %v", err)
	}
	if _, err := dataBlockPart(message.NewDataBlock(message.NewURLSource("https://example.com/audio.wav", "audio/wav")), "openai"); !isCapabilityError(err, asmodel.ModelCapabilityGeneration) {
		t.Fatalf("OpenAI URL audio should return capability error, got %v", err)
	}
	if part, err := dataBlockPart(nil); err != nil || part != nil {
		t.Fatalf("nil data block should be ignored: part=%#v err=%v", part, err)
	}
	if part, err := dataBlockPart(&message.DataBlock{}); err != nil || part != nil {
		t.Fatalf("nil data source should be ignored: part=%#v err=%v", part, err)
	}
}

func isCapabilityError(err error, capability asmodel.ModelCapability) bool {
	var capabilityErr *asmodel.CapabilityError
	return errors.As(err, &capabilityErr) && capabilityErr.Capability == capability
}

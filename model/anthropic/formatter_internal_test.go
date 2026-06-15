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

package anthropic

import (
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
)

func TestHintContentBlocksFormatsNestedContentAndErrors(t *testing.T) {
	t.Parallel()

	plain, err := hintContentBlocks(message.NewHintBlock("plain hint"))
	if err != nil || len(plain) != 1 {
		t.Fatalf("plain hint mismatch: len=%d err=%v", len(plain), err)
	}

	nested, err := hintContentBlocks(message.NewHintBlock(message.ContentBlockList{
		message.NewTextBlock("look"),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "image/png")),
		message.NewDataBlock(message.NewURLSource("https://example.test/image.png", "image/png")),
	}))
	if err != nil || len(nested) != 3 {
		t.Fatalf("nested hint mismatch: len=%d err=%v", len(nested), err)
	}

	if _, err := hintContentBlocks(message.NewHintBlock(message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain")))); err == nil ||
		!strings.Contains(err.Error(), "unsupported base64 media type") {
		t.Fatalf("text data hint should return media type error, got %v", err)
	}
	if _, err := hintContentBlocks(message.NewHintBlock(message.ContentBlockList{message.NewThinkingBlock("hidden")})); err == nil ||
		!strings.Contains(err.Error(), "unsupported hint content block") {
		t.Fatalf("unsupported nested hint block should return error, got %v", err)
	}
}

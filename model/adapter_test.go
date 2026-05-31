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

package model_test

import (
	"errors"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestNormalizeChatResponseBuildsRootResponse(t *testing.T) {
	t.Parallel()

	resp, err := asmodel.NormalizeChatResponse([]asmodel.ResponsePart{
		{Kind: asmodel.PartText, ID: "text-1", Text: "hello"},
		{Kind: asmodel.PartThinking, ID: "thinking-1", Thinking: "plan"},
		{Kind: asmodel.PartToolCall, ID: "call-1", ToolName: "Read", ToolInput: `{"path":"README.md"}`},
	}, asmodel.WithResponseUsage(&asmodel.ChatUsage{InputTokens: 1, OutputTokens: 2}))
	if err != nil {
		t.Fatalf("NormalizeChatResponse returned error: %v", err)
	}
	if !resp.IsLast || len(resp.Content) != 3 {
		t.Fatalf("unexpected normalized response: %#v", resp)
	}
	if resp.Content[0].(*message.TextBlock).ID != "text-1" {
		t.Fatalf("text id not preserved: %#v", resp.Content[0])
	}
	if resp.Content[2].(*message.ToolCallBlock).Name != "Read" {
		t.Fatalf("tool call not normalized: %#v", resp.Content[2])
	}
}

func TestNormalizeErrorWrapsProviderMetadata(t *testing.T) {
	t.Parallel()

	cause := errors.New("transport failed")
	err := asmodel.NormalizeError("openai", cause, asmodel.WithStatusCode(429), asmodel.WithErrorCode("rate_limit"))

	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("normalized error should expose ProviderError, got %T", err)
	}
	if providerErr.Provider != "openai" || providerErr.StatusCode != 429 || providerErr.Code != "rate_limit" {
		t.Fatalf("provider metadata not preserved: %#v", providerErr)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("normalized error should unwrap cause")
	}
}

func TestNormalizeChatResponseCoversDataAndOptions(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{"nested": map[string]any{"key": "value"}}
	resp, err := asmodel.NormalizeChatResponse([]asmodel.ResponsePart{
		{Kind: asmodel.PartBase64Data, ID: "data-1", Data: "abc", MediaType: "image/png", Name: "image"},
		{Kind: asmodel.PartURLData, ID: "url-1", URL: "https://example.com/a.png", MediaType: "image/png"},
	},
		asmodel.WithResponseIsLast(false),
		asmodel.WithResponseMetadata(metadata),
	)
	if err != nil {
		t.Fatalf("NormalizeChatResponse returned error: %v", err)
	}
	if resp.IsLast || resp.Metadata["nested"].(map[string]any)["key"] != "value" {
		t.Fatalf("normalization options not applied: %#v", resp)
	}
	resp.Metadata["nested"].(map[string]any)["key"] = "changed"
	if metadata["nested"].(map[string]any)["key"] != "value" {
		t.Fatalf("metadata should be deep copied, got %#v", metadata)
	}
	data := resp.Content[0].(*message.DataBlock)
	if data.Name == nil || *data.Name != "image" {
		t.Fatalf("data block name not preserved: %#v", data)
	}
	if resp.Content[1].(*message.DataBlock).Source.SourceType() != "url" {
		t.Fatalf("url data block not normalized: %#v", resp.Content[1])
	}
}

func TestNormalizeChatResponseRejectsUnknownPartKind(t *testing.T) {
	t.Parallel()

	_, err := asmodel.NormalizeChatResponse([]asmodel.ResponsePart{{Kind: "unknown"}})
	if err == nil {
		t.Fatal("unknown part kind should return error")
	}
}

func TestProviderErrorFormattingBranches(t *testing.T) {
	t.Parallel()

	if got := (*asmodel.ProviderError)(nil).Error(); got != "<nil>" {
		t.Fatalf("nil provider error string mismatch: %q", got)
	}
	if (*asmodel.ProviderError)(nil).Unwrap() != nil {
		t.Fatal("nil provider error unwrap should return nil")
	}
	if err := asmodel.NormalizeError("openai", nil); err != nil {
		t.Fatalf("nil provider error should normalize to nil: %v", err)
	}

	tests := []*asmodel.ProviderError{
		{Provider: "openai", StatusCode: 500, Message: "server"},
		{Provider: "openai", Code: "bad_request", Message: "bad"},
		{Message: "unknown"},
	}
	for _, providerErr := range tests {
		if providerErr.Error() == "" {
			t.Fatalf("provider error should format non-empty string: %#v", providerErr)
		}
	}
}

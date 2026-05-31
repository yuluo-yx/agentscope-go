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

package message_test

import (
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
)

func TestMessageFactoriesAndQueries(t *testing.T) {
	t.Parallel()

	userMessage, err := message.NewUserMessage(
		"user",
		"hello",
		message.WithMessageMetadata(map[string]any{"trace": map[string]any{"id": "1"}}),
		message.WithMessageCreatedAt("2026-01-01T00:00:00Z"),
		message.WithMessageFinishedAt("2026-01-01T00:00:01Z"),
	)
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	if userMessage.Role != message.RoleUser {
		t.Fatalf("unexpected role: %q", userMessage.Role)
	}
	if got := userMessage.GetTextContent(" "); got == nil || *got != "hello" {
		t.Fatalf("unexpected text content: %#v", got)
	}
	if !userMessage.HasContentBlocks("text") {
		t.Fatal("message should report text blocks")
	}
	if !userMessage.HasContentBlocks() {
		t.Fatal("message should report any content blocks")
	}
	if userMessage.Metadata["trace"].(map[string]any)["id"] != "1" {
		t.Fatalf("metadata not set: %#v", userMessage.Metadata)
	}

	clone := userMessage.Clone()
	clone.Content[0].(*message.TextBlock).Text = "changed"
	clone.Metadata["trace"].(map[string]any)["id"] = "changed"
	got := userMessage.GetTextContent("")
	if got == nil || *got != "hello" {
		t.Fatalf("clone mutation changed original message: %#v", got)
	}
	if userMessage.Metadata["trace"].(map[string]any)["id"] != "1" {
		t.Fatalf("clone mutation changed original metadata: %#v", userMessage.Metadata)
	}
}

func TestAssistantAndSystemFactories(t *testing.T) {
	t.Parallel()

	assistant, err := message.NewAssistantMessage("assistant", []message.ContentBlock{
		message.NewHintBlock("hint"),
	}, message.WithMessageUsage(message.Usage{InputTokens: 1, OutputTokens: 2}))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	if assistant.Role != message.RoleAssistant || assistant.Usage.OutputTokens != 2 {
		t.Fatalf("unexpected assistant message: %#v", assistant)
	}

	system, err := message.NewSystemMessage("system", []*message.TextBlock{
		message.NewTextBlock("policy"),
	})
	if err != nil {
		t.Fatalf("NewSystemMessage returned error: %v", err)
	}
	if system.Role != message.RoleSystem || system.FinishedAt == nil {
		t.Fatalf("unexpected system message: %#v", system)
	}

	if _, err := message.NewAssistantMessage("assistant", 123); err == nil {
		t.Fatal("NewAssistantMessage should return an error for unsupported content")
	}
	if _, err := message.NewSystemMessage("system", []message.ContentBlock{
		message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
	}); err == nil {
		t.Fatal("NewSystemMessage should return an error for invalid system content")
	}

	if _, err := message.NewUserMessage("user", 123); err == nil {
		t.Fatal("unsupported content type should return an error")
	}
	if _, err := message.NewMessage("nobody", message.Role("unknown"), nil); err == nil {
		t.Fatal("unknown role should return an error")
	}

	msg, err := message.NewMessage("assistant", message.RoleAssistant, nil, message.WithMessageID(""), message.WithMessageCreatedAt(""))
	if err != nil {
		t.Fatalf("NewMessage should fill empty id and timestamp: %v", err)
	}
	if msg.ID == "" || msg.CreatedAt == "" {
		t.Fatalf("NewMessage did not fill empty fields: %#v", msg)
	}
	if got := msg.GetTextContent(""); got != nil {
		t.Fatalf("empty message should not have text content: %#v", got)
	}
	if msg.FindBlock("text", "missing") != nil {
		t.Fatal("FindBlock should return nil for missing blocks")
	}
}

func TestMustMessageFactoriesPanicOnInvalidContent(t *testing.T) {
	t.Parallel()

	assistant := message.MustAssistantMessage("assistant", "hello")
	if assistant.Role != message.RoleAssistant {
		t.Fatalf("unexpected assistant role: %q", assistant.Role)
	}

	system := message.MustSystemMessage("system", "policy")
	if system.Role != message.RoleSystem || system.FinishedAt == nil {
		t.Fatalf("unexpected system message: %#v", system)
	}

	assertPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s should panic", name)
			}
		}()
		fn()
	}

	assertPanic("MustAssistantMessage", func() {
		message.MustAssistantMessage("assistant", 123)
	})
	assertPanic("MustSystemMessage", func() {
		message.MustSystemMessage("system", []message.ContentBlock{
			message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
		})
	})
}

func TestMessageValidatesRoleContent(t *testing.T) {
	t.Parallel()

	_, err := message.NewUserMessage("user", []message.ContentBlock{
		message.NewThinkingBlock("hidden"),
	})
	if err == nil {
		t.Fatal("NewUserMessage should reject thinking blocks")
	}

	_, err = message.NewMessage("system", message.RoleSystem, []message.ContentBlock{
		message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
	})
	if err == nil {
		t.Fatal("NewMessage should reject data blocks in system messages")
	}
}

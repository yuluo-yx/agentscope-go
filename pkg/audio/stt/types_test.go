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

package stt_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/audio/stt"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

func TestResponseOptionsCloneAudioBlockAndSegments(t *testing.T) {
	t.Parallel()

	rawAudio := []byte{0x01, 0x02, 0x03, 0x04}
	block := stt.NewAudioBlock(rawAudio, "audio/wav")
	source := block.Source.(*message.Base64Source)
	if source.Data != base64.StdEncoding.EncodeToString(rawAudio) || source.MediaType != "audio/wav" {
		t.Fatalf("audio block source mismatch: %#v", source)
	}
	defaultBlock := stt.NewAudioBlock(rawAudio, "")
	if defaultBlock.Source.(*message.Base64Source).MediaType != "application/octet-stream" {
		t.Fatalf("empty media type should default to application/octet-stream: %#v", defaultBlock.Source)
	}

	metadata := map[string]any{"nested": map[string]any{"provider": "dashscope"}}
	usage := &stt.Usage{
		InputTokens:   3,
		OutputTokens:  5,
		AudioDuration: 2 * time.Second,
		Time:          10 * time.Millisecond,
		Metadata:      map[string]any{"request_id": "req-1"},
	}
	resp := stt.NewResponse(
		"hello world",
		true,
		stt.WithResponseSegments([]stt.Segment{{Text: "hello", Start: time.Second, End: 1500 * time.Millisecond}}),
		stt.WithResponseLanguage("en"),
		stt.WithResponseID("stt-1"),
		stt.WithResponseCreatedAt("2026-01-02T03:04:05Z"),
		stt.WithResponseUsage(usage),
		stt.WithResponseMetadata(metadata),
	)
	if resp.ID != "stt-1" || resp.CreatedAt != "2026-01-02T03:04:05Z" || resp.Type != stt.ResponseType || !resp.IsLast {
		t.Fatalf("response defaults/options mismatch: %#v", resp)
	}
	if resp.Text != "hello world" || resp.Language != "en" || len(resp.Segments) != 1 {
		t.Fatalf("response content mismatch: %#v", resp)
	}
	if resp.Usage == usage || resp.Usage.Type != stt.UsageTypeSTT {
		t.Fatalf("usage should be cloned and default to STT type: %#v", resp.Usage)
	}
	metadata["nested"].(map[string]any)["provider"] = "mutated"
	if resp.Metadata["nested"].(map[string]any)["provider"] != "dashscope" {
		t.Fatalf("metadata should be deep-copied: %#v", resp.Metadata)
	}

	cloned := resp.Clone()
	cloned.Segments[0].Text = "clone"
	cloned.Metadata["nested"].(map[string]any)["provider"] = "clone-only"
	if resp.Segments[0].Text != "hello" || resp.Metadata["nested"].(map[string]any)["provider"] != "dashscope" {
		t.Fatalf("Clone should deep-copy nested metadata and segments: original=%#v clone=%#v", resp, cloned)
	}
}

func TestRequestUsageAndResponseErrorOptionsClone(t *testing.T) {
	t.Parallel()

	if (*stt.Usage)(nil).Clone() != nil {
		t.Fatal("nil usage clone should be nil")
	}
	usage := (&stt.Usage{Metadata: map[string]any{"provider": "dashscope"}}).Clone()
	if usage.Type != stt.UsageTypeSTT || usage.Metadata["provider"] != "dashscope" {
		t.Fatalf("usage clone should default type and copy metadata: %#v", usage)
	}

	request := stt.Request{
		Audio:      stt.NewAudioBlock([]byte("raw"), "audio/wav"),
		Parameters: map[string]any{"language": "en"},
		Metadata:   map[string]any{"trace": map[string]any{"id": "1"}},
	}
	clone := request.Clone()
	clone.Audio.Source.(*message.Base64Source).Data = "changed"
	clone.Parameters["language"] = "zh"
	clone.Metadata["trace"].(map[string]any)["id"] = "2"
	if request.Audio.Source.(*message.Base64Source).Data == "changed" ||
		request.Parameters["language"] != "en" ||
		request.Metadata["trace"].(map[string]any)["id"] != "1" {
		t.Fatalf("request clone should deep-copy fields: original=%#v clone=%#v", request, clone)
	}

	response := stt.NewResponse(
		"",
		false,
		stt.WithResponseID(""),
		stt.WithResponseCreatedAt(""),
		stt.WithResponseUsage(&stt.Usage{}),
		stt.WithResponseMetadata(nil),
	)
	if response.ID == "" || response.CreatedAt == "" || response.Type != stt.ResponseType || response.Metadata == nil {
		t.Fatalf("response should restore empty defaults: %#v", response)
	}
	if response.Usage == nil || response.Usage.Type != stt.UsageTypeSTT {
		t.Fatalf("usage type should default to STT: %#v", response.Usage)
	}

	err := assertErr("stream failed")
	errorResponse := stt.NewResponse("", true, stt.WithResponseError(err))
	if !errors.Is(errorResponse.Error, err) || !errorResponse.IsLast || errorResponse.Text != "" {
		t.Fatalf("response error option mismatch: %#v", errorResponse)
	}
	if clone := (*stt.Response)(nil).Clone(); clone != nil {
		t.Fatalf("nil response clone should be nil: %#v", clone)
	}
}

func TestSessionRequestCloneAndInterfaceShape(t *testing.T) {
	t.Parallel()

	request := stt.SessionRequest{
		Parameters: map[string]any{"language": "zh"},
		Metadata:   map[string]any{"trace": map[string]any{"id": "1"}},
	}
	clone := request.Clone()
	clone.Parameters["language"] = "en"
	clone.Metadata["trace"].(map[string]any)["id"] = "2"
	if request.Parameters["language"] != "zh" || request.Metadata["trace"].(map[string]any)["id"] != "1" {
		t.Fatalf("session request clone should deep-copy fields: original=%#v clone=%#v", request, clone)
	}

	var _ stt.Session = (*assertSession)(nil)
	var _ stt.Model = (*assertModel)(nil)
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

type assertSession struct{}

func (s *assertSession) ID() string { return "session-1" }

func (s *assertSession) Responses() <-chan stt.Response {
	ch := make(chan stt.Response)
	close(ch)
	return ch
}

func (s *assertSession) Push(context.Context, *message.DataBlock) error { return nil }

func (s *assertSession) Commit(context.Context) error { return nil }

func (s *assertSession) Finish(context.Context) error { return nil }

func (s *assertSession) Close(context.Context) error { return nil }

type assertModel struct{}

func (m *assertModel) Name() string { return "assert:model" }

func (m *assertModel) Realtime() bool { return true }

func (m *assertModel) Recognize(context.Context, stt.Request) (<-chan stt.Response, error) {
	ch := make(chan stt.Response)
	close(ch)
	return ch, nil
}

func (m *assertModel) NewSession(context.Context, stt.SessionRequest) (stt.Session, error) {
	return &assertSession{}, nil
}

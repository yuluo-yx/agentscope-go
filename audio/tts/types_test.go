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

package tts_test

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/audio/tts"
	"github.com/yuluo-yx/agentscope-go/message"
)

func TestResponseOptionsCloneAudioBlockAndWAV(t *testing.T) {
	t.Parallel()

	rawPCM := []byte{0x01, 0x02, 0x03, 0x04}
	block := tts.NewAudioBlock(rawPCM, "audio/wav")
	source := block.Source.(*message.Base64Source)
	if source.Data != base64.StdEncoding.EncodeToString(rawPCM) || source.MediaType != "audio/wav" {
		t.Fatalf("audio block source mismatch: %#v", source)
	}

	metadata := map[string]any{"nested": map[string]any{"voice": "Cherry"}}
	usage := &tts.Usage{
		InputTokens:  3,
		OutputTokens: 5,
		Time:         10 * time.Millisecond,
		Metadata:     map[string]any{"provider": "dashscope"},
	}
	resp := tts.NewResponse(
		block,
		true,
		tts.WithResponseID("tts-1"),
		tts.WithResponseCreatedAt("2026-01-02T03:04:05Z"),
		tts.WithResponseUsage(usage),
		tts.WithResponseMetadata(metadata),
	)
	if resp.ID != "tts-1" || resp.CreatedAt != "2026-01-02T03:04:05Z" || resp.Type != tts.ResponseType || !resp.IsLast {
		t.Fatalf("response defaults/options mismatch: %#v", resp)
	}
	if resp.Usage == usage || resp.Usage.Type != tts.UsageTypeTTS {
		t.Fatalf("usage should be cloned and default to TTS type: %#v", resp.Usage)
	}
	metadata["nested"].(map[string]any)["voice"] = "Mutated"
	if resp.Metadata["nested"].(map[string]any)["voice"] != "Cherry" {
		t.Fatalf("metadata should be deep-copied: %#v", resp.Metadata)
	}

	cloned := resp.Clone()
	cloned.Metadata["nested"].(map[string]any)["voice"] = "CloneOnly"
	cloned.Content.Source.(*message.Base64Source).Data = "changed"
	if resp.Metadata["nested"].(map[string]any)["voice"] != "Cherry" || resp.Content.Source.(*message.Base64Source).Data != source.Data {
		t.Fatalf("Clone should deep-copy nested metadata and content: original=%#v clone=%#v", resp, cloned)
	}

	streamingHeader := tts.StreamingWAVHeader(24000, 1, 16)
	if len(streamingHeader) != 44 || string(streamingHeader[:4]) != "RIFF" || string(streamingHeader[8:12]) != "WAVE" {
		t.Fatalf("streaming WAV header is malformed: %q", streamingHeader[:12])
	}
	if got := binary.LittleEndian.Uint32(streamingHeader[24:28]); got != 24000 {
		t.Fatalf("sample rate mismatch in streaming header: %d", got)
	}

	wrapped := tts.WrapPCMAsWAV(rawPCM, 24000, 1, 16)
	if len(wrapped) != 44+len(rawPCM) || string(wrapped[:4]) != "RIFF" || string(wrapped[8:12]) != "WAVE" {
		t.Fatalf("wrapped WAV data is malformed: len=%d header=%q", len(wrapped), wrapped[:12])
	}
	if got := binary.LittleEndian.Uint32(wrapped[4:8]); got != uint32(36+len(rawPCM)) {
		t.Fatalf("chunk size mismatch: %d", got)
	}
	if got := binary.LittleEndian.Uint32(wrapped[40:44]); got != uint32(len(rawPCM)) {
		t.Fatalf("data chunk size mismatch: %d", got)
	}
	if payload := wrapped[44:]; string(payload) != string(rawPCM) {
		t.Fatalf("PCM payload mismatch: %#v", payload)
	}
}

func TestAudioDefaultsAndResponseFallbacks(t *testing.T) {
	t.Parallel()

	block := tts.NewAudioBlock([]byte("raw"), "")
	source := block.Source.(*message.Base64Source)
	if source.MediaType != "application/octet-stream" {
		t.Fatalf("empty media type should default to octet stream: %#v", source)
	}

	wrapped := tts.WrapPCMAsWAV(nil, 0, 0, 0)
	if got := binary.LittleEndian.Uint32(wrapped[24:28]); got != 24000 {
		t.Fatalf("default sample rate mismatch: %d", got)
	}
	if got := binary.LittleEndian.Uint16(wrapped[22:24]); got != 1 {
		t.Fatalf("default channels mismatch: %d", got)
	}
	if got := binary.LittleEndian.Uint16(wrapped[34:36]); got != 16 {
		t.Fatalf("default bits per sample mismatch: %d", got)
	}

	response := tts.NewResponse(
		nil,
		false,
		tts.WithResponseID(""),
		tts.WithResponseCreatedAt(""),
		tts.WithResponseUsage(&tts.Usage{}),
		tts.WithResponseMetadata(nil),
	)
	if response.ID == "" || response.CreatedAt == "" || response.Type != tts.ResponseType || response.Metadata == nil {
		t.Fatalf("response should restore empty defaults: %#v", response)
	}
	if response.Usage == nil || response.Usage.Type != tts.UsageTypeTTS {
		t.Fatalf("usage type should default to TTS: %#v", response.Usage)
	}
}

func TestRequestUsageAndResponseErrorOptionsClone(t *testing.T) {
	t.Parallel()

	if (*tts.Usage)(nil).Clone() != nil {
		t.Fatal("nil usage clone should be nil")
	}
	usage := (&tts.Usage{Metadata: map[string]any{"provider": "dashscope"}}).Clone()
	if usage.Type != tts.UsageTypeTTS || usage.Metadata["provider"] != "dashscope" {
		t.Fatalf("usage clone should default type and copy metadata: %#v", usage)
	}

	request := tts.Request{
		Text:       "hello",
		Parameters: map[string]any{"voice": "Cherry"},
		Metadata:   map[string]any{"trace": map[string]any{"id": "1"}},
	}
	clone := request.Clone()
	clone.Parameters["voice"] = "Serena"
	clone.Metadata["trace"].(map[string]any)["id"] = "2"
	if request.Parameters["voice"] != "Cherry" || request.Metadata["trace"].(map[string]any)["id"] != "1" {
		t.Fatalf("request clone should deep-copy maps: original=%#v clone=%#v", request, clone)
	}

	err := assertErr("stream failed")
	response := tts.NewResponse(nil, true, tts.WithResponseError(err))
	if !errors.Is(response.Error, err) || !response.IsLast || response.Content != nil {
		t.Fatalf("response error option mismatch: %#v", response)
	}
	if clone := (*tts.Response)(nil).Clone(); clone != nil {
		t.Fatalf("nil response clone should be nil: %#v", clone)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

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

package dashscope_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/tts"
	"github.com/yuluo-yx/agentscope-go/tts/dashscope"
)

type realtimeServerMessage struct {
	Type    string         `json:"type"`
	EventID string         `json:"event_id"`
	Session map[string]any `json:"session"`
	Text    string         `json:"text"`
}

func TestRealtimeModelUsesDashScopeWebSocketProtocol(t *testing.T) {
	t.Parallel()

	received := make(chan realtimeServerMessage, 4)
	server := newRealtimeTTSServer(t, func(t *testing.T, conn *websocket.Conn, r *http.Request) {
		if r.URL.Path != "/api-ws/v1/realtime" {
			t.Fatalf("unexpected realtime path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("model") != "qwen3-tts-flash-realtime" {
			t.Fatalf("unexpected model query: %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer dash-key" {
			t.Fatalf("authorization header mismatch: %q", r.Header.Get("Authorization"))
		}
		writeRealtimeEvent(t, conn, map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess-1"},
		})
		for {
			var msg realtimeServerMessage
			if err := conn.ReadJSON(&msg); err != nil {
				t.Fatalf("read websocket message: %v", err)
			}
			received <- msg
			switch msg.Type {
			case "input_text_buffer.commit":
				writeRealtimeEvent(t, conn, map[string]any{
					"type":  "response.audio.delta",
					"delta": base64.StdEncoding.EncodeToString([]byte{1, 2}),
				})
			case "session.finish":
				writeRealtimeEvent(t, conn, map[string]any{
					"type":  "response.audio.delta",
					"delta": base64.StdEncoding.EncodeToString([]byte{3, 4}),
				})
				writeRealtimeEvent(t, conn, map[string]any{"type": "session.finished"})
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	model, err := dashscope.NewRealtimeModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"qwen3-tts-flash-realtime",
		dashscope.WithRealtimeParameters(dashscope.Parameters{Voice: "Cherry"}),
		dashscope.WithRealtimeStream(true),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel returned error: %v", err)
	}
	defer func() { _ = model.Close(context.Background()) }()

	if !model.Realtime() {
		t.Fatal("RealtimeModel should report realtime capability")
	}
	if err := model.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	update := receiveRealtimeMessage(t, received)
	if update.Type != "session.update" || update.EventID == "" {
		t.Fatalf("session update mismatch: %#v", update)
	}
	if update.Session["voice"] != "Cherry" || update.Session["mode"] != "server_commit" ||
		update.Session["response_format"] != "pcm" || update.Session["sample_rate"] != float64(24000) {
		t.Fatalf("session parameters mismatch: %#v", update.Session)
	}

	response, err := model.Push(ctx, "Hello")
	if err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	if response == nil || response.Content != nil {
		t.Fatalf("Push should return an empty response before audio arrives: %#v", response)
	}
	appendMsg := receiveRealtimeMessage(t, received)
	if appendMsg.Type != "input_text_buffer.append" || appendMsg.Text != "Hello" || appendMsg.EventID == "" {
		t.Fatalf("append message mismatch: %#v", appendMsg)
	}

	chunks, err := model.Synthesize(ctx, tts.Request{})
	if err != nil {
		t.Fatalf("Synthesize returned error: %v", err)
	}
	commit := receiveRealtimeMessage(t, received)
	finish := receiveRealtimeMessage(t, received)
	if commit.Type != "input_text_buffer.commit" || finish.Type != "session.finish" {
		t.Fatalf("finalization messages mismatch: commit=%#v finish=%#v", commit, finish)
	}

	got := collectTTSResponses(chunks)
	if len(got) == 0 || !got[len(got)-1].IsLast {
		t.Fatalf("expected a terminal realtime chunk, got %#v", got)
	}
	for i := 0; i < len(got)-1; i++ {
		if got[i].IsLast {
			t.Fatalf("only the final realtime chunk should be terminal: %#v", got)
		}
	}
	pcm := decodeRealtimePCM(t, got)
	if string(pcm) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("realtime PCM mismatch: %#v", pcm)
	}
	if got[len(got)-1].Metadata["provider"] != "dashscope" || got[len(got)-1].Metadata["session_id"] != "sess-1" {
		t.Fatalf("final metadata mismatch: %#v", got[len(got)-1].Metadata)
	}
}

func TestRealtimeModelBuffersColdStartText(t *testing.T) {
	t.Parallel()

	received := make(chan realtimeServerMessage, 4)
	server := newRealtimeTTSServer(t, func(t *testing.T, conn *websocket.Conn, r *http.Request) {
		_ = r
		writeRealtimeEvent(t, conn, map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess-cold"},
		})
		for {
			var msg realtimeServerMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			received <- msg
			if msg.Type == "session.finish" {
				writeRealtimeEvent(t, conn, map[string]any{"type": "session.finished"})
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	model, err := dashscope.NewRealtimeModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"qwen3-tts-flash-realtime",
		dashscope.WithRealtimeColdStartLength(10),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel returned error: %v", err)
	}
	defer func() { _ = model.Close(context.Background()) }()
	if err := model.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	_ = receiveRealtimeMessage(t, received)

	if _, err := model.Push(ctx, "Hel"); err != nil {
		t.Fatalf("first Push returned error: %v", err)
	}
	select {
	case msg := <-received:
		t.Fatalf("cold-start text should be buffered, got message: %#v", msg)
	case <-time.After(80 * time.Millisecond):
	}
	if _, err := model.Push(ctx, "lo world"); err != nil {
		t.Fatalf("second Push returned error: %v", err)
	}
	appendMsg := receiveRealtimeMessage(t, received)
	if appendMsg.Type != "input_text_buffer.append" || appendMsg.Text != "Hello world" {
		t.Fatalf("cold-start append mismatch: %#v", appendMsg)
	}

	chunks, err := model.Synthesize(ctx, tts.Request{})
	if err != nil {
		t.Fatalf("Synthesize returned error: %v", err)
	}
	_ = receiveRealtimeMessage(t, received)
	_ = receiveRealtimeMessage(t, received)
	got := collectTTSResponses(chunks)
	if len(got) != 1 || !got[0].IsLast || got[0].Content != nil {
		t.Fatalf("empty final response mismatch after cold-start flush: %#v", got)
	}
}

func TestRealtimeModelNonStreamingFlushesBufferedText(t *testing.T) {
	t.Parallel()

	received := make(chan realtimeServerMessage, 4)
	server := newRealtimeTTSServer(t, func(t *testing.T, conn *websocket.Conn, r *http.Request) {
		if r.URL.Path != "/custom/realtime" || r.URL.Query().Get("model") != "qwen3-tts-flash-realtime" {
			t.Fatalf("unexpected realtime URL: path=%s query=%s", r.URL.Path, r.URL.RawQuery)
		}
		writeRealtimeEvent(t, conn, map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess-nonstream"},
		})
		for {
			var msg realtimeServerMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			received <- msg
			if msg.Type == "session.finish" {
				writeRealtimeEvent(t, conn, map[string]any{
					"type":  "response.audio.delta",
					"delta": base64.StdEncoding.EncodeToString([]byte{9, 8, 7}),
				})
				writeRealtimeEvent(t, conn, map[string]any{"type": "session.finished"})
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	model, err := dashscope.NewRealtimeModel(
		dashscope.NewCredential("dash-key"),
		"qwen3-tts-flash-realtime",
		dashscope.WithRealtimeEndpoint(" "+strings.Replace(server.URL, "http://", "ws://", 1)+"/custom/realtime "),
		dashscope.WithRealtimeStream(false),
		dashscope.WithRealtimeColdStartWords(3),
		dashscope.WithRealtimeConnectTimeout(2*time.Second),
		dashscope.WithRealtimeParameters(dashscope.Parameters{
			Voice: "Serena",
			Extra: map[string]any{"language_type": "auto", "voice": "ignored"},
		}),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel returned error: %v", err)
	}
	defer func() { _ = model.Close(context.Background()) }()
	if model.Name() != "dashscope:qwen3-tts-flash-realtime" {
		t.Fatalf("model name mismatch: %s", model.Name())
	}
	if err := model.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	update := receiveRealtimeMessage(t, received)
	if update.Session["voice"] != "Serena" || update.Session["language_type"] != "auto" {
		t.Fatalf("session update should merge extra parameters without overriding defaults: %#v", update.Session)
	}
	if response, err := model.Push(ctx, "one two"); err != nil || response == nil || response.Content != nil {
		t.Fatalf("Push should buffer below cold-start word threshold: response=%#v err=%v", response, err)
	}
	select {
	case msg := <-received:
		t.Fatalf("text should remain buffered below word threshold, got %#v", msg)
	case <-time.After(80 * time.Millisecond):
	}

	chunks, err := model.Synthesize(ctx, tts.Request{Text: " three"})
	if err != nil {
		t.Fatalf("Synthesize returned error: %v", err)
	}
	appendMsg := receiveRealtimeMessage(t, received)
	commitMsg := receiveRealtimeMessage(t, received)
	finishMsg := receiveRealtimeMessage(t, received)
	if appendMsg.Type != "input_text_buffer.append" || appendMsg.Text != "one two three" ||
		commitMsg.Type != "input_text_buffer.commit" || finishMsg.Type != "session.finish" {
		t.Fatalf("non-stream finalization messages mismatch: append=%#v commit=%#v finish=%#v", appendMsg, commitMsg, finishMsg)
	}
	got := collectTTSResponses(chunks)
	if len(got) != 1 || !got[0].IsLast {
		t.Fatalf("non-streaming synthesize should return one final response: %#v", got)
	}
	if pcm := decodeRealtimePCM(t, got); string(pcm) != string([]byte{9, 8, 7}) {
		t.Fatalf("non-streaming PCM mismatch: %#v", pcm)
	}
}

func TestRealtimeModelValidationAndProviderErrorEvents(t *testing.T) {
	t.Parallel()

	if _, err := dashscope.NewRealtimeModel(dashscope.NewCredential(""), "qwen3-tts-flash-realtime"); err == nil {
		t.Fatal("NewRealtimeModel should reject empty API key")
	}
	if _, err := dashscope.NewRealtimeModel(dashscope.NewCredential("key"), ""); err == nil {
		t.Fatal("NewRealtimeModel should reject empty model")
	}
	if got := (*dashscope.RealtimeModel)(nil).Name(); got != "dashscope:<nil>" {
		t.Fatalf("nil realtime model name mismatch: %q", got)
	}

	server := newRealtimeTTSServer(t, func(t *testing.T, conn *websocket.Conn, r *http.Request) {
		_ = r
		writeRealtimeEvent(t, conn, map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess-error"},
		})
		for {
			var msg realtimeServerMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.Type == "session.finish" {
				writeRealtimeEvent(t, conn, map[string]any{
					"type":    "error",
					"code":    "InvalidInput",
					"message": "bad text",
				})
				return
			}
		}
	})
	defer server.Close()

	model, err := dashscope.NewRealtimeModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"qwen3-tts-flash-realtime",
		dashscope.WithRealtimeStream(false),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel returned error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := model.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	chunks, err := model.Synthesize(ctx, tts.Request{Text: "bad"})
	if err != nil {
		t.Fatalf("Synthesize returned error before reading provider event: %v", err)
	}
	got := collectTTSResponses(chunks)
	if len(got) != 1 || got[0].Error == nil || !strings.Contains(got[0].Error.Error(), "bad text") {
		t.Fatalf("provider error event should produce terminal error response: %#v", got)
	}
}

func newRealtimeTTSServer(
	t *testing.T,
	handler func(*testing.T, *websocket.Conn, *http.Request),
) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		handler(t, conn, r)
	}))
}

func writeRealtimeEvent(t *testing.T, conn *websocket.Conn, event map[string]any) {
	t.Helper()
	if err := conn.WriteJSON(event); err != nil {
		t.Fatalf("write websocket event: %v", err)
	}
}

func receiveRealtimeMessage(t *testing.T, ch <-chan realtimeServerMessage) realtimeServerMessage {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for realtime websocket message")
		return realtimeServerMessage{}
	}
}

func decodeTTSBase64(t *testing.T, response tts.Response) []byte {
	t.Helper()
	if response.Content == nil {
		t.Fatal("response content is nil")
	}
	source, ok := response.Content.Source.(*message.Base64Source)
	if !ok {
		t.Fatalf("response source should be Base64Source: %#v", response.Content.Source)
	}
	data, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		t.Fatalf("decode response base64: %v", err)
	}
	return data
}

func decodeRealtimePCM(t *testing.T, responses []tts.Response) []byte {
	t.Helper()
	var pcm []byte
	for index, response := range responses {
		if response.Content == nil {
			continue
		}
		data := decodeTTSBase64(t, response)
		if index == 0 {
			if len(data) < 44 || string(data[:4]) != "RIFF" ||
				binary.LittleEndian.Uint32(data[24:28]) != 24000 {
				t.Fatalf("first realtime chunk should include a 24kHz WAV header: %#v", data)
			}
			data = data[44:]
		}
		pcm = append(pcm, data...)
	}
	return pcm
}

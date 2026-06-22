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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/yuluo-yx/agentscope-go/pkg/audio/stt"
	"github.com/yuluo-yx/agentscope-go/pkg/audio/stt/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
)

type realtimeSTTClientMessage struct {
	Type    string         `json:"type"`
	EventID string         `json:"event_id"`
	Session map[string]any `json:"session"`
	Audio   string         `json:"audio"`
}

func TestRealtimeModelSessionUsesQwenASRProtocol(t *testing.T) {
	t.Parallel()

	received := make(chan realtimeSTTClientMessage, 4)
	server := newRealtimeSTTServer(t, func(t *testing.T, conn *websocket.Conn, r *http.Request) {
		if r.URL.Path != "/api-ws/v1/realtime" {
			t.Fatalf("unexpected realtime path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("model") != "qwen3-asr-flash-realtime" {
			t.Fatalf("unexpected model query: %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer dash-key" {
			t.Fatalf("authorization header mismatch: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-DashScope-WorkSpace") != "workspace-1" {
			t.Fatalf("workspace header mismatch: %q", r.Header.Get("X-DashScope-WorkSpace"))
		}
		if r.Header.Get("X-DashScope-DataInspection") != "disable" {
			t.Fatalf("data inspection header mismatch: %q", r.Header.Get("X-DashScope-DataInspection"))
		}

		writeSTTRealtimeEvent(t, conn, map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess-1"},
		})
		for {
			var msg realtimeSTTClientMessage
			if err := conn.ReadJSON(&msg); err != nil {
				t.Fatalf("read websocket message: %v", err)
			}
			received <- msg
			switch msg.Type {
			case "session.update":
				writeSTTRealtimeEvent(t, conn, map[string]any{
					"type":    "session.updated",
					"session": map[string]any{"id": "sess-1"},
				})
			case "input_audio_buffer.append":
				writeSTTRealtimeEvent(t, conn, map[string]any{
					"type":          "conversation.item.input_audio_transcription.text",
					"item_id":       "item-1",
					"content_index": 0,
					"language":      "zh",
					"emotion":       "neutral",
					"text":          "你",
					"stash":         "好",
				})
			case "session.finish":
				writeSTTRealtimeEvent(t, conn, map[string]any{
					"type":          "conversation.item.input_audio_transcription.completed",
					"item_id":       "item-1",
					"content_index": 0,
					"language":      "zh",
					"emotion":       "neutral",
					"transcript":    "你好世界",
				})
				writeSTTRealtimeEvent(t, conn, map[string]any{"type": "session.finished"})
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	model, err := dashscope.NewRealtimeModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"qwen3-asr-flash-realtime",
		dashscope.WithRealtimeParameters(dashscope.RealtimeParameters{Language: "zh"}),
		dashscope.WithRealtimeWorkspace("workspace-1"),
		dashscope.WithRealtimeDataInspection("disable"),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel returned error: %v", err)
	}
	if !model.Realtime() || model.Name() != "dashscope:qwen3-asr-flash-realtime" {
		t.Fatalf("realtime model metadata mismatch: name=%s realtime=%v", model.Name(), model.Realtime())
	}

	session, err := model.NewSession(ctx, stt.SessionRequest{})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	defer func() { _ = session.Close(context.Background()) }()

	update := receiveSTTRealtimeMessage(t, received)
	if update.Type != "session.update" || update.EventID == "" {
		t.Fatalf("session update mismatch: %#v", update)
	}
	if update.Session["input_audio_format"] != "pcm" || update.Session["sample_rate"] != float64(16000) {
		t.Fatalf("session audio defaults mismatch: %#v", update.Session)
	}
	transcription := update.Session["input_audio_transcription"].(map[string]any)
	if transcription["language"] != "zh" {
		t.Fatalf("transcription settings mismatch: %#v", transcription)
	}
	turnDetection := update.Session["turn_detection"].(map[string]any)
	if turnDetection["type"] != "server_vad" || turnDetection["threshold"] != float64(0) ||
		turnDetection["silence_duration_ms"] != float64(400) {
		t.Fatalf("turn detection settings mismatch: %#v", turnDetection)
	}

	if err := session.Push(ctx, stt.NewAudioBlock([]byte("pcm"), "audio/pcm")); err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	appendMsg := receiveSTTRealtimeMessage(t, received)
	if appendMsg.Type != "input_audio_buffer.append" || appendMsg.Audio != base64.StdEncoding.EncodeToString([]byte("pcm")) {
		t.Fatalf("append message mismatch: %#v", appendMsg)
	}

	partial := receiveSTTResponse(t, session.Responses())
	if partial.Text != "你好" || partial.IsLast || partial.Language != "zh" ||
		partial.Metadata["stash"] != "好" || partial.Metadata["confirmed_text"] != "你" {
		t.Fatalf("partial response mismatch: %#v", partial)
	}
	if session.ID() != "sess-1" {
		t.Fatalf("session id mismatch: %q", session.ID())
	}

	if err := session.Finish(ctx); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
	finishMsg := receiveSTTRealtimeMessage(t, received)
	if finishMsg.Type != "session.finish" || finishMsg.EventID == "" {
		t.Fatalf("finish message mismatch: %#v", finishMsg)
	}
	final := receiveSTTResponse(t, session.Responses())
	if final.Text != "你好世界" || !final.IsLast || final.Language != "zh" ||
		final.Metadata["item_id"] != "item-1" || final.Metadata["provider"] != "dashscope" {
		t.Fatalf("final response mismatch: %#v", final)
	}
	assertSTTResponsesClosed(t, session.Responses())
}

func TestRealtimeModelManualSessionCommitAndProviderErrors(t *testing.T) {
	t.Parallel()

	received := make(chan realtimeSTTClientMessage, 4)
	server := newRealtimeSTTServer(t, func(t *testing.T, conn *websocket.Conn, r *http.Request) {
		_ = r
		writeSTTRealtimeEvent(t, conn, map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess-manual"},
		})
		for {
			var msg realtimeSTTClientMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			received <- msg
			switch msg.Type {
			case "input_audio_buffer.commit":
				writeSTTRealtimeEvent(t, conn, map[string]any{
					"type":    "input_audio_buffer.committed",
					"item_id": "item-manual",
				})
				writeSTTRealtimeEvent(t, conn, map[string]any{
					"type":          "conversation.item.input_audio_transcription.failed",
					"item_id":       "item-manual",
					"content_index": 0,
					"error": map[string]any{
						"code":    "InvalidAudio",
						"message": "bad audio",
						"param":   "audio",
					},
				})
			case "session.finish":
				writeSTTRealtimeEvent(t, conn, map[string]any{"type": "session.finished"})
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	model, err := dashscope.NewRealtimeModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"qwen3-asr-flash-realtime",
		dashscope.WithRealtimeParameters(dashscope.RealtimeParameters{Mode: dashscope.RealtimeModeManual}),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel returned error: %v", err)
	}
	session, err := model.NewSession(ctx, stt.SessionRequest{})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	defer func() { _ = session.Close(context.Background()) }()

	update := receiveSTTRealtimeMessage(t, received)
	if update.Type != "session.update" {
		t.Fatalf("session update mismatch: %#v", update)
	}
	if _, exists := update.Session["turn_detection"]; !exists || update.Session["turn_detection"] != nil {
		t.Fatalf("manual mode should send null turn_detection: %#v", update.Session)
	}
	if err := session.Push(ctx, message.NewDataBlock(message.NewURLSource("https://example.test/a.wav", "audio/wav"))); err == nil {
		t.Fatal("Push should reject URL audio for realtime sessions")
	}
	if err := session.Push(ctx, stt.NewAudioBlock([]byte("bad"), "audio/pcm")); err != nil {
		t.Fatalf("Push returned error: %v", err)
	}
	_ = receiveSTTRealtimeMessage(t, received)
	if err := session.Commit(ctx); err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	commit := receiveSTTRealtimeMessage(t, received)
	if commit.Type != "input_audio_buffer.commit" || commit.EventID == "" {
		t.Fatalf("commit message mismatch: %#v", commit)
	}

	response := receiveSTTResponse(t, session.Responses())
	if response.Error == nil || !response.IsLast || !strings.Contains(response.Error.Error(), "bad audio") {
		t.Fatalf("failed transcription should produce terminal error response: %#v", response)
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(response.Error, &providerErr) || providerErr.Code != "InvalidAudio" {
		t.Fatalf("provider error metadata mismatch: %#v", response.Error)
	}
}

func TestRealtimeModelRecognizeOneShotManualSession(t *testing.T) {
	t.Parallel()

	received := make(chan realtimeSTTClientMessage, 8)
	server := newRealtimeSTTServer(t, func(t *testing.T, conn *websocket.Conn, r *http.Request) {
		if r.URL.Query().Get("model") != "qwen3-asr-flash-realtime" {
			t.Fatalf("model query mismatch: %s", r.URL.RawQuery)
		}
		writeSTTRealtimeEvent(t, conn, map[string]any{
			"type":    "session.created",
			"session": map[string]any{"id": "sess-recognize"},
		})
		for {
			var msg realtimeSTTClientMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			received <- msg
			switch msg.Type {
			case "session.update":
				writeSTTRealtimeEvent(t, conn, map[string]any{
					"type":    "session.updated",
					"session": map[string]any{"id": "sess-recognize"},
				})
			case "input_audio_buffer.commit":
				writeSTTRealtimeEvent(t, conn, map[string]any{
					"type":       "conversation.item.input_audio_transcription.completed",
					"language":   "en",
					"transcript": "recognized",
				})
			case "session.finish":
				writeSTTRealtimeEvent(t, conn, map[string]any{"type": "session.finished"})
				return
			}
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	model, err := dashscope.NewRealtimeModel(
		dashscope.NewCredential("dash-key"),
		"qwen3-asr-flash-realtime",
		dashscope.WithRealtimeEndpoint(server.URL+"/api-ws/v1/realtime"),
		dashscope.WithRealtimeDialer(websocket.DefaultDialer),
		dashscope.WithRealtimeConnectTimeout(time.Second),
		dashscope.WithRealtimeParameters(dashscope.RealtimeParameters{Mode: dashscope.RealtimeModeManual}),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel returned error: %v", err)
	}
	responses, err := model.Recognize(ctx, stt.Request{
		Audio:    stt.NewAudioBlock([]byte("pcm"), "audio/pcm"),
		Metadata: map[string]any{"request_id": "req-recognize"},
	})
	if err != nil {
		t.Fatalf("Recognize returned error: %v", err)
	}
	got := collectSTTResponses(responses)
	if len(got) != 1 || got[0].Text != "recognized" || !got[0].IsLast ||
		got[0].Language != "en" || got[0].Metadata["request_id"] != "req-recognize" {
		t.Fatalf("recognize response mismatch: %#v", got)
	}
	wantTypes := []string{"session.update", "input_audio_buffer.append", "input_audio_buffer.commit", "session.finish"}
	for _, want := range wantTypes {
		msg := receiveSTTRealtimeMessage(t, received)
		if msg.Type != want {
			t.Fatalf("message type = %s, want %s", msg.Type, want)
		}
	}
}

func newRealtimeSTTServer(
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

func writeSTTRealtimeEvent(t *testing.T, conn *websocket.Conn, event map[string]any) {
	t.Helper()
	if err := conn.WriteJSON(event); err != nil {
		t.Fatalf("write websocket event: %v", err)
	}
}

func receiveSTTRealtimeMessage(t *testing.T, ch <-chan realtimeSTTClientMessage) realtimeSTTClientMessage {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for realtime websocket message")
		return realtimeSTTClientMessage{}
	}
}

func receiveSTTResponse(t *testing.T, ch <-chan stt.Response) stt.Response {
	t.Helper()
	select {
	case response, ok := <-ch:
		if !ok {
			t.Fatal("response channel closed unexpectedly")
		}
		return response
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for realtime stt response")
		return stt.Response{}
	}
}

func assertSTTResponsesClosed(t *testing.T, ch <-chan stt.Response) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("response channel should be closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response channel close")
	}
}

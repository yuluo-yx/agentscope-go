package server

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"strings"

	v1 "kratos/api/chat/v1"
	"kratos/internal/conf"
	"kratos/internal/service"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, chat *service.ChatService, logger log.Logger) *khttp.Server {
	var opts = []khttp.ServerOption{
		khttp.Middleware(
			recovery.Recovery(),
		),
	}
	if c.Http.Network != "" {
		opts = append(opts, khttp.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, khttp.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, khttp.Timeout(c.Http.Timeout.AsDuration()))
	}
	srv := khttp.NewServer(opts...)
	v1.RegisterChatHTTPServer(srv, chat)
	srv.HandleFunc("/stream-chat", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		err := chat.StreamChatEvents(r.Context(), strings.TrimSpace(r.URL.Query().Get("prompt")), func(reply *v1.ChatStreamReply) error {
			return writeSSE(w, "", reply)
		})
		if err != nil {
			_ = writeSSE(w, "error", map[string]any{"error": err.Error()})
		}
	})
	srv.HandleFunc("/stream-chat-tool", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		err := chat.StreamChatToolEvents(r.Context(), strings.TrimSpace(r.URL.Query().Get("prompt")), func(reply *v1.ChatStreamReply) error {
			return writeSSE(w, "", reply)
		})
		if err != nil {
			_ = writeSSE(w, "error", map[string]any{"error": err.Error()})
		}
	})
	srv.HandleFunc("/agent/stream-chat", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		err := chat.AgentStreamChatEvents(r.Context(), strings.TrimSpace(r.URL.Query().Get("prompt")), func(reply *v1.AgentStreamReply) error {
			return writeSSE(w, reply.GetEvent(), reply)
		})
		if err != nil {
			_ = writeSSE(w, "error", map[string]any{"error": err.Error()})
		}
	})
	return srv
}

func writeSSE(w stdhttp.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	if flusher, ok := w.(stdhttp.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

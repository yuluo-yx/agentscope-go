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

package middleware

import (
	"context"
	"fmt"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/tts"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// TTSMiddleware appends synthesized audio data blocks after assistant text blocks.
type TTSMiddleware struct {
	model tts.Model
}

// NewTTSMiddleware creates middleware backed by a TTS model.
func NewTTSMiddleware(model tts.Model) *TTSMiddleware {
	return &TTSMiddleware{model: model}
}

// MiddlewareName returns the middleware name.
func (m *TTSMiddleware) MiddlewareName() string {
	if m == nil {
		return "tts"
	}
	return "tts"
}

// OnReply preserves the original reply events and appends TTS data-block events for text output.
func (m *TTSMiddleware) OnReply(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	_ = agent
	_ = input
	if m == nil || m.model == nil {
		return next(ctx)
	}
	events, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return nil, fmt.Errorf("agentscope/middleware: nil event stream")
	}

	out := make(chan message.Event)
	go func() {
		defer close(out)
		processor := ttsEventProcessor{model: m.model}
		if m.model.Realtime() {
			if err := m.model.Connect(ctx); err != nil {
				processor.err = err
			}
			defer func() {
				_ = m.model.Close(context.WithoutCancel(ctx))
			}()
		}
		for event := range events {
			out <- event
			processor.handle(ctx, out, event)
		}
		processor.closeAudioBlock(out)
	}()
	return out, nil
}

type ttsEventProcessor struct {
	model     tts.Model
	text      strings.Builder
	replyID   string
	blockID   string
	mediaType string
	err       error
}

func (p *ttsEventProcessor) handle(ctx context.Context, out chan<- message.Event, event message.Event) {
	if p.err != nil {
		return
	}
	switch e := event.(type) {
	case *message.TextBlockDeltaEvent:
		p.text.WriteString(e.Delta)
		if p.model.Realtime() && e.Delta != "" {
			response, err := p.model.Push(ctx, e.Delta)
			if err != nil {
				p.err = err
				return
			}
			p.emitResponse(out, e.ReplyID(), response)
		}
	case *message.TextBlockEndEvent:
		if p.model.Realtime() {
			p.flushRealtime(ctx, out, e.ReplyID())
			p.text.Reset()
			return
		}
		text := strings.TrimSpace(p.text.String())
		p.text.Reset()
		if text == "" {
			return
		}
		p.emitSynthesized(ctx, out, e.ReplyID(), tts.Request{Text: text})
		p.closeAudioBlock(out)
	}
}

func (p *ttsEventProcessor) flushRealtime(ctx context.Context, out chan<- message.Event, replyID string) {
	p.emitSynthesized(ctx, out, replyID, tts.Request{})
	p.closeAudioBlock(out)
}

func (p *ttsEventProcessor) emitSynthesized(ctx context.Context, out chan<- message.Event, replyID string, request tts.Request) {
	if p.err != nil {
		return
	}
	responses, err := p.model.Synthesize(ctx, request)
	if err != nil {
		p.err = err
		return
	}
	for response := range responses {
		p.emitResponse(out, replyID, &response)
		if p.err != nil {
			return
		}
	}
}

func (p *ttsEventProcessor) emitResponse(out chan<- message.Event, replyID string, response *tts.Response) {
	if response == nil || response.Content == nil || response.Error != nil {
		if response != nil && response.Error != nil {
			p.err = response.Error
		}
		return
	}
	source, ok := response.Content.Source.(*message.Base64Source)
	if !ok {
		return
	}
	mediaType := source.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	if p.blockID == "" {
		p.blockID = response.Content.ID
		if p.blockID == "" {
			p.blockID = utils.NewID()
		}
		p.replyID = replyID
		p.mediaType = mediaType
		out <- message.NewDataBlockStartEvent(replyID, p.blockID, mediaType)
	}
	out <- message.NewDataBlockDeltaEvent(replyID, p.blockID, source.Data, p.mediaType)
}

func (p *ttsEventProcessor) closeAudioBlock(out chan<- message.Event) {
	if p.blockID == "" {
		return
	}
	out <- message.NewDataBlockEndEvent(p.replyID, p.blockID)
	p.replyID = ""
	p.blockID = ""
	p.mediaType = ""
}

var _ agentpkg.ReplyMiddleware = (*TTSMiddleware)(nil)

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

package service

import (
	"context"
	v1 "kratos/api/chat/v1"
	"kratos/internal/biz"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

// ChatService adapts the AgentScope Go usecase to the Kratos proto contract.
type ChatService struct {
	v1.UnimplementedChatServer

	uc *biz.ChatUsecase
}

// NewChatService creates the Kratos service layer for chat APIs.
func NewChatService(uc *biz.ChatUsecase) *ChatService {
	return &ChatService{uc: uc}
}

// Ping implements the health endpoint.
func (s *ChatService) Ping(context.Context, *v1.PingRequest) (*v1.PingReply, error) {
	return &v1.PingReply{Message: "pong"}, nil
}

// Chat implements the unary ChatModel endpoint.
func (s *ChatService) Chat(ctx context.Context, req *v1.ChatRequest) (*v1.ChatReply, error) {
	content, err := s.uc.Chat(ctx, req.GetPrompt())
	if err != nil {
		return nil, err
	}
	text := content.GetTextContent()
	if text == nil {
		text = new(string)
	}
	return &v1.ChatReply{Content: contentToProto(content), Text: *text}, nil
}

// StreamChat implements the gRPC streaming ChatModel endpoint.
func (s *ChatService) StreamChat(req *v1.ChatRequest, stream v1.Chat_StreamChatServer) error {
	return s.StreamChatEvents(stream.Context(), req.GetPrompt(), func(reply *v1.ChatStreamReply) error {
		return stream.Send(reply)
	})
}

// StreamChatEvents emits ChatModel stream replies for HTTP SSE and gRPC transports.
func (s *ChatService) StreamChatEvents(ctx context.Context, prompt string, emit func(*v1.ChatStreamReply) error) error {
	return s.uc.StreamChat(ctx, prompt, func(content message.ContentBlockList, final bool) error {
		text := content.GetTextContent()
		if text == nil {
			text = new(string)
		}
		return emit(&v1.ChatStreamReply{Content: contentToProto(content), Text: *text, Final: final})
	})
}

// StreamChatTool implements the gRPC streaming ChatModel tool-call endpoint.
func (s *ChatService) StreamChatTool(req *v1.ChatRequest, stream v1.Chat_StreamChatToolServer) error {
	return s.StreamChatToolEvents(stream.Context(), req.GetPrompt(), func(reply *v1.ChatStreamReply) error {
		return stream.Send(reply)
	})
}

// StreamChatToolEvents emits the two-step ChatModel tool-call stream for HTTP SSE and gRPC transports.
func (s *ChatService) StreamChatToolEvents(ctx context.Context, prompt string, emit func(*v1.ChatStreamReply) error) error {
	return s.uc.StreamChatTool(ctx, prompt, func(content message.ContentBlockList, final bool) error {
		text := content.GetTextContent()
		if text == nil {
			text = new(string)
		}
		return emit(&v1.ChatStreamReply{Content: contentToProto(content), Text: *text, Final: final})
	})
}

// AgentChat implements the unary Agent endpoint.
func (s *ChatService) AgentChat(ctx context.Context, req *v1.ChatRequest) (*v1.ChatReply, error) {
	content, err := s.uc.AgentChat(ctx, req.GetPrompt())
	if err != nil {
		return nil, err
	}
	text := content.GetTextContent()
	if text == nil {
		text = new(string)
	}
	return &v1.ChatReply{Content: contentToProto(content), Text: *text}, nil
}

// AgentStreamChat implements the gRPC streaming Agent endpoint.
func (s *ChatService) AgentStreamChat(req *v1.ChatRequest, stream v1.Chat_AgentStreamChatServer) error {
	return s.AgentStreamChatEvents(stream.Context(), req.GetPrompt(), func(reply *v1.AgentStreamReply) error {
		return stream.Send(reply)
	})
}

// AgentStreamChatEvents emits Agent events for HTTP SSE and gRPC transports.
func (s *ChatService) AgentStreamChatEvents(ctx context.Context, prompt string, emit func(*v1.AgentStreamReply) error) error {
	return s.uc.AgentStreamChat(ctx, prompt, func(event message.Event) error {
		switch e := event.(type) {
		case *message.ToolCallStartEvent:
			return emit(&v1.AgentStreamReply{Event: "tool_call_start", Tool: e.ToolCallName})
		case *message.ToolResultEndEvent:
			return emit(&v1.AgentStreamReply{Event: "tool_result_end", State: string(e.State)})
		case *message.TextBlockDeltaEvent:
			return emit(&v1.AgentStreamReply{Event: "text_delta", Delta: e.Delta})
		default:
			return nil
		}
	})
}

// StructuredOutput implements the structured JSON example endpoint.
func (s *ChatService) StructuredOutput(ctx context.Context, req *v1.ChatRequest) (*v1.StructuredReply, error) {
	output, err := s.uc.StructuredOutput(ctx, req.GetPrompt())
	if err != nil {
		return nil, err
	}
	data, err := structpb.NewStruct(output.Output)
	if err != nil {
		return nil, err
	}
	return &v1.StructuredReply{Output: data, Raw: output.Raw}, nil
}

func contentToProto(blocks message.ContentBlockList) []*v1.ContentBlock {
	out := make([]*v1.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch b := block.(type) {
		case *message.TextBlock:
			out = append(out, &v1.ContentBlock{Type: b.Type, Text: b.Text, Id: b.ID})
		case *message.ToolCallBlock:
			out = append(out, &v1.ContentBlock{Type: b.Type, Id: b.ID, Name: b.Name, State: string(b.State)})
		case *message.ToolResultBlock:
			content := &v1.ContentBlock{Type: b.Type, Id: b.ID, Name: b.Name, State: string(b.State)}
			if text := b.Output.Blocks.GetTextContent(); text != nil {
				content.Text = *text
			}
			out = append(out, content)
		default:
			out = append(out, &v1.ContentBlock{Type: block.BlockType(), Id: block.BlockID()})
		}
	}
	return out
}

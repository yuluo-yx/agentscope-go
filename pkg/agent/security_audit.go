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

package agent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

const maxSecurityAuditErrorLen = 512

// SecurityAuditEventType identifies security-relevant Agent events.
type SecurityAuditEventType string

const (
	SecurityAuditEventPermissionRequired SecurityAuditEventType = "permission_required"
	SecurityAuditEventPermissionDenied   SecurityAuditEventType = "permission_denied"
	SecurityAuditEventToolExecutionError SecurityAuditEventType = "tool_execution_error"
)

// SecurityAuditEvent carries metadata for permission and tool execution audit records.
type SecurityAuditEvent struct {
	Type      SecurityAuditEventType `json:"type"`
	ToolName  string                 `json:"tool_name,omitempty"`
	MCPName   string                 `json:"mcp_name,omitempty"`
	ReplyID   string                 `json:"reply_id,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// SecurityAuditLogger receives security audit events. Implementations must not log full tool inputs.
type SecurityAuditLogger interface {
	LogSecurityAudit(context.Context, SecurityAuditEvent)
}

// SecurityAuditFunc adapts a function into a SecurityAuditLogger.
type SecurityAuditFunc func(context.Context, SecurityAuditEvent)

// LogSecurityAudit implements SecurityAuditLogger.
func (f SecurityAuditFunc) LogSecurityAudit(ctx context.Context, event SecurityAuditEvent) {
	f(ctx, event)
}

type slogSecurityAuditLogger struct{}

func (slogSecurityAuditLogger) LogSecurityAudit(ctx context.Context, event SecurityAuditEvent) {
	slog.Default().LogAttrs(
		ctx,
		slog.LevelDebug,
		"agentscope security audit",
		slog.String("type", string(event.Type)),
		slog.String("tool_name", event.ToolName),
		slog.String("mcp_name", event.MCPName),
		slog.String("reply_id", event.ReplyID),
		slog.String("session_id", event.SessionID),
		slog.String("error", event.Error),
	)
}

func (a *Agent) auditToolSecurityEvent(ctx context.Context, eventType SecurityAuditEventType, tool Tool, toolCall *message.ToolCallBlock, errText string) {
	if a == nil || a.securityAuditLogger == nil {
		return
	}
	event := SecurityAuditEvent{
		Type:  eventType,
		Error: securityAuditErrorSummary(errText),
	}
	if tool != nil {
		event.ToolName = tool.Name()
		event.MCPName = tool.MCPName()
	}
	if event.ToolName == "" && toolCall != nil {
		event.ToolName = toolCall.Name
	}
	if a.state != nil {
		event.ReplyID = a.state.ReplyID
		event.SessionID = a.state.SessionID
	}
	a.securityAuditLogger.LogSecurityAudit(ctx, event)
}

func securityAuditErrorSummary(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxSecurityAuditErrorLen {
		return text
	}
	return text[:maxSecurityAuditErrorLen]
}

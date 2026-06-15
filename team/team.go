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

package team

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/yuluo-yx/agentscope-go/agent"
	agenterrors "github.com/yuluo-yx/agentscope-go/errors"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// TeamRole determines which team tools are attached to an agent.
type TeamRole string

const (
	// TeamRoleLeader attaches leader tools: TeamCreate, AgentCreate, TeamSay, and TeamDelete.
	TeamRoleLeader TeamRole = "leader"
	// TeamRoleWorker attaches worker tools: TeamSay.
	TeamRoleWorker TeamRole = "worker"
)

// TeamWorkerRequest describes a worker requested by AgentCreate.
type TeamWorkerRequest struct {
	Team           TeamSnapshot
	Leader         *agent.Agent
	Name           string
	Description    string
	Prompt         string
	SystemPrompt   string
	PermissionMode permission.PermissionMode
	// PermissionContext is the worker permission context after inheriting the leader session.
	PermissionContext *permission.Context
}

// TeamWorkerFactory creates a worker agent for AgentCreate.
type TeamWorkerFactory func(context.Context, TeamWorkerRequest) (*agent.Agent, error)

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// Manager stores process-local team membership and inbox messages.
type Manager struct {
	mu sync.Mutex

	workerFactory TeamWorkerFactory
	workerModel   asmodel.ChatModel
	workerOptions []agent.AgentOption

	participants map[string]*teamParticipant
	teams        map[string]*teamRecord
	sessionTeams map[string]string
	inbox        map[string][]*message.Message
}

// TeamManager is kept for compatibility with earlier examples.
//
// Deprecated: use Manager.
type TeamManager = Manager

// TeamManagerOption is kept for compatibility with earlier examples.
//
// Deprecated: use ManagerOption.
type TeamManagerOption = ManagerOption

type teamParticipant struct {
	ID          string
	Name        string
	Description string
	Role        TeamRole
	SessionID   string
	Agent       *agent.Agent
}

type teamRecord struct {
	ID          string
	Name        string
	Description string
	LeaderID    string
	MemberIDs   []string
}

// TeamSnapshot is a read-only view of a team.
type TeamSnapshot struct {
	ID          string
	Name        string
	Description string
	Leader      TeamMemberSnapshot
	Members     []TeamMemberSnapshot
}

// TeamMemberSnapshot is a read-only view of a team participant.
type TeamMemberSnapshot struct {
	ID          string
	Name        string
	Description string
	Role        TeamRole
	SessionID   string
}

// WithTeamWorkerFactory sets the worker factory used by AgentCreate.
func WithTeamWorkerFactory(factory TeamWorkerFactory) ManagerOption {
	return func(manager *Manager) {
		manager.workerFactory = factory
	}
}

// WithTeamWorkerModel sets the model used by the default worker factory.
func WithTeamWorkerModel(model asmodel.ChatModel) ManagerOption {
	return func(manager *Manager) {
		manager.workerModel = model
	}
}

// WithTeamWorkerOptions sets options appended by the default worker factory.
func WithTeamWorkerOptions(opts ...agent.AgentOption) ManagerOption {
	return func(manager *Manager) {
		manager.workerOptions = append([]agent.AgentOption(nil), opts...)
	}
}

// NewManager creates a process-local team manager.
func NewManager(opts ...ManagerOption) *Manager {
	manager := &Manager{
		participants: map[string]*teamParticipant{},
		teams:        map[string]*teamRecord{},
		sessionTeams: map[string]string{},
		inbox:        map[string][]*message.Message{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(manager)
		}
	}
	return manager
}

// NewTeamManager creates a process-local team manager.
//
// Deprecated: use NewManager.
func NewTeamManager(opts ...ManagerOption) *Manager {
	return NewManager(opts...)
}

// WithTeam registers an agent with a Manager and attaches team tools.
func WithTeam(manager *Manager, role TeamRole) agent.AgentOption {
	return func(agentValue *agent.Agent) error {
		if manager == nil {
			return agenterrors.NewDeveloperError("agent team manager is nil")
		}
		if err := manager.RegisterAgent(agentValue, role, ""); err != nil {
			return err
		}
		kit, err := manager.Toolkit(role)
		if err != nil {
			return err
		}
		return agent.WithAdditionalToolkit(kit)(agentValue)
	}
}

// RegisterAgent makes an existing agent addressable by team tools.
func (m *Manager) RegisterAgent(agentValue *agent.Agent, role TeamRole, description string) error {
	if m == nil {
		return agenterrors.NewDeveloperError("agent team manager is nil")
	}
	if agentValue == nil {
		return agenterrors.NewDeveloperError("team agent is nil")
	}
	agentState := agentValue.AgentState()
	if agentState == nil {
		return agenterrors.NewDeveloperError("team agent is nil")
	}
	if role == "" {
		role = TeamRoleLeader
	}
	sessionID := strings.TrimSpace(agentState.SessionID)
	if sessionID == "" {
		return agenterrors.NewDeveloperError("team agent session id is empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	participant := m.participants[sessionID]
	if participant == nil {
		participant = &teamParticipant{ID: utils.NewID()}
		m.participants[sessionID] = participant
	}
	participant.Name = agentValue.AgentName()
	participant.Description = description
	participant.Role = role
	participant.SessionID = sessionID
	participant.Agent = agentValue
	return nil
}

// Toolkit returns the team tools visible to a role.
func (m *Manager) Toolkit(role TeamRole) (*astool.Toolkit, error) {
	if m == nil {
		return nil, agenterrors.NewDeveloperError("agent team manager is nil")
	}
	tools := []astool.Tool{m.teamSayTool(role)}
	if role != TeamRoleWorker {
		tools = []astool.Tool{
			m.teamCreateTool(),
			m.agentCreateTool(),
			m.teamSayTool(TeamRoleLeader),
			m.teamDeleteTool(),
		}
	}
	return astool.NewToolkit(tools...)
}

// TeamForSession returns the team snapshot for a session.
func (m *Manager) TeamForSession(sessionID string) (*TeamSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	teamID, ok := m.sessionTeams[sessionID]
	if !ok {
		return nil, false
	}
	snapshot, ok := m.snapshotLocked(teamID)
	return snapshot, ok
}

// Team returns a team snapshot by id.
func (m *Manager) Team(teamID string) (*TeamSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked(teamID)
}

// DrainInbox observes pending team messages on the agent and clears its inbox.
func (m *Manager) DrainInbox(ctx context.Context, agentValue *agent.Agent) error {
	if agentValue == nil {
		return agenterrors.NewDeveloperError("team drain agent is nil")
	}
	agentState := agentValue.AgentState()
	if agentState == nil {
		return agenterrors.NewDeveloperError("team drain agent is nil")
	}
	messages := m.PendingMessages(agentState.SessionID)
	if len(messages) == 0 {
		return nil
	}
	return agentValue.Observe(ctx, messages)
}

// PendingMessages returns and clears pending team messages for a session.
func (m *Manager) PendingMessages(sessionID string) []*message.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	messages := m.inbox[sessionID]
	delete(m.inbox, sessionID)
	out := make([]*message.Message, 0, len(messages))
	for _, msg := range messages {
		if msg != nil {
			out = append(out, msg.Clone())
		}
	}
	return out
}

// PendingAgents returns registered agents with pending team messages.
func (m *Manager) PendingAgents() []*agent.Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	agents := []*agent.Agent{}
	for sessionID, inbox := range m.inbox {
		if len(inbox) == 0 {
			continue
		}
		if participant := m.participants[sessionID]; participant != nil && participant.Agent != nil {
			agents = append(agents, participant.Agent)
		}
	}
	return agents
}

func (m *Manager) createTeam(state *asstate.AgentState, name, description string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	caller, err := m.callerLocked(state)
	if err != nil {
		return "", err
	}
	if _, exists := m.sessionTeams[caller.SessionID]; exists {
		return "", fmt.Errorf("TeamCreate: this session is already part of a team")
	}
	team := &teamRecord{
		ID:          utils.NewID(),
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		LeaderID:    caller.SessionID,
		MemberIDs:   []string{},
	}
	if team.Name == "" {
		return "", fmt.Errorf("TeamCreate: name is required")
	}
	m.teams[team.ID] = team
	m.sessionTeams[caller.SessionID] = team.ID
	return fmt.Sprintf("Team %s (%s) created. You are the leader. Use AgentCreate to add members, then TeamSay to coordinate them.", team.ID, team.Name), nil
}

func (m *Manager) createAgent(ctx context.Context, state *asstate.AgentState, name, description, prompt string, mode permission.PermissionMode) (string, error) {
	m.mu.Lock()
	caller, team, err := m.leaderTeamLocked(state)
	if err != nil {
		m.mu.Unlock()
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		m.mu.Unlock()
		return "", fmt.Errorf("AgentCreate: name is required")
	}
	if err = m.ensureUniqueNameLocked(team, name); err != nil {
		m.mu.Unlock()
		return "", err
	}
	snapshot, _ := m.snapshotLocked(team.ID)
	request := TeamWorkerRequest{
		Team:              *snapshot,
		Leader:            caller.Agent,
		Name:              name,
		Description:       description,
		Prompt:            prompt,
		SystemPrompt:      buildWorkerSystemPrompt(team.Name, team.Description, name, description),
		PermissionMode:    mode,
		PermissionContext: inheritLeaderPermissionContext(state.PermissionContext, mode),
	}
	factory := m.workerFactory
	if factory == nil {
		factory = m.defaultWorkerFactory
	}
	m.mu.Unlock()

	worker, err := factory(ctx, request)
	if err != nil {
		return "", err
	}
	workerState := worker.AgentState()
	if worker == nil || workerState == nil {
		return "", fmt.Errorf("AgentCreate: worker factory returned nil agent")
	}
	if err := m.RegisterAgent(worker, TeamRoleWorker, description); err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	team = m.teams[request.Team.ID]
	if team == nil {
		return "", fmt.Errorf("AgentCreate: team %s no longer exists", request.Team.ID)
	}
	participant := m.participants[workerState.SessionID]
	participant.Name = name
	participant.Description = description
	participant.Role = TeamRoleWorker
	team.MemberIDs = append(team.MemberIDs, workerState.SessionID)
	m.sessionTeams[workerState.SessionID] = team.ID
	m.deliverLocked(workerState.SessionID, caller.Name, prompt)
	return fmt.Sprintf("Member %q added to team %q.", name, team.Name), nil
}

func (m *Manager) say(state *asstate.AgentState, content, to string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	caller, team, err := m.currentTeamLocked(state)
	if err != nil {
		return "", err
	}
	recipients := []string{}
	if strings.TrimSpace(to) == "" {
		for _, sessionID := range append([]string{team.LeaderID}, team.MemberIDs...) {
			if sessionID != caller.SessionID {
				recipients = append(recipients, sessionID)
			}
		}
	} else {
		target, ok := m.memberByNameLocked(team, to)
		if !ok {
			return "", fmt.Errorf("TeamSay: no team member is named %q", to)
		}
		if target.SessionID == caller.SessionID {
			return "", fmt.Errorf("TeamSay: cannot send a message to yourself")
		}
		recipients = append(recipients, target.SessionID)
	}
	for _, sessionID := range recipients {
		m.deliverLocked(sessionID, caller.Name, content)
	}
	target := "broadcast"
	if strings.TrimSpace(to) != "" {
		target = fmt.Sprintf("member %q", to)
	}
	return fmt.Sprintf("Delivered to %d recipient(s) (%s).", len(recipients), target), nil
}

func (m *Manager) deleteTeam(state *asstate.AgentState) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, team, err := m.leaderTeamLocked(state)
	if err != nil {
		return "", err
	}
	for _, memberID := range team.MemberIDs {
		delete(m.sessionTeams, memberID)
		delete(m.inbox, memberID)
		delete(m.participants, memberID)
	}
	delete(m.sessionTeams, team.LeaderID)
	delete(m.teams, team.ID)
	return fmt.Sprintf("Team %s dissolved. All members deleted; your session is no longer leading any team.", team.ID), nil
}

func (m *Manager) defaultWorkerFactory(ctx context.Context, request TeamWorkerRequest) (*agent.Agent, error) {
	if m.workerModel == nil {
		return nil, fmt.Errorf("AgentCreate: no worker factory or worker model configured")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := append([]agent.AgentOption{}, m.workerOptions...)
	opts = append(opts, WithTeam(m, TeamRoleWorker))
	worker, err := agent.NewAgent(request.Name, request.SystemPrompt, m.workerModel, opts...)
	if err != nil {
		return nil, err
	}
	if worker.AgentState() != nil {
		switch {
		case request.PermissionContext != nil:
			worker.AgentState().PermissionContext = mergeWorkerPermissionContext(worker.AgentState().PermissionContext, request.PermissionContext)
		case request.PermissionMode != "":
			worker.AgentState().PermissionContext = permission.NewContext(request.PermissionMode)
		}
	}
	return worker, nil
}

func inheritLeaderPermissionContext(leader *permission.Context, mode permission.PermissionMode) *permission.Context {
	var inherited *permission.Context
	if leader == nil {
		inherited = permission.NewContext(permission.ModeDefault)
	} else {
		inherited = leader.Clone()
	}
	if inherited == nil {
		inherited = permission.NewContext(permission.ModeDefault)
	}
	if mode != "" {
		inherited.Mode = mode
	}
	if inherited.Mode == "" {
		inherited.Mode = permission.ModeDefault
	}
	return inherited
}

func mergeWorkerPermissionContext(base, inherited *permission.Context) *permission.Context {
	if inherited == nil {
		if base == nil {
			return permission.NewContext(permission.ModeDefault)
		}
		return base.Clone()
	}
	if base == nil {
		return inherited.Clone()
	}
	merged := base.Clone()
	if merged == nil {
		merged = permission.NewContext(inherited.Mode)
	}
	merged.Mode = inherited.Mode
	for key, dir := range inherited.WorkingDirectories {
		if _, exists := merged.WorkingDirectories[key]; !exists {
			merged.WorkingDirectories[key] = dir
		}
	}
	appendRuleMap(merged.AllowRules, inherited.AllowRules)
	appendRuleMap(merged.DenyRules, inherited.DenyRules)
	appendRuleMap(merged.AskRules, inherited.AskRules)
	return merged
}

func appendRuleMap(dst, src map[string][]permission.Rule) {
	for toolName, rules := range src {
		dst[toolName] = append(dst[toolName], rules...)
	}
}

func (m *Manager) callerLocked(state *asstate.AgentState) (*teamParticipant, error) {
	if state == nil {
		return nil, fmt.Errorf("team tool requires agent state")
	}
	caller := m.participants[state.SessionID]
	if caller == nil {
		return nil, fmt.Errorf("team tool caller session %q is not registered", state.SessionID)
	}
	return caller, nil
}

func (m *Manager) currentTeamLocked(state *asstate.AgentState) (*teamParticipant, *teamRecord, error) {
	caller, err := m.callerLocked(state)
	if err != nil {
		return nil, nil, err
	}
	teamID, ok := m.sessionTeams[caller.SessionID]
	if !ok {
		return nil, nil, fmt.Errorf("team tool caller is not in any team")
	}
	team := m.teams[teamID]
	if team == nil {
		return nil, nil, fmt.Errorf("team %s no longer exists", teamID)
	}
	return caller, team, nil
}

func (m *Manager) leaderTeamLocked(state *asstate.AgentState) (*teamParticipant, *teamRecord, error) {
	caller, team, err := m.currentTeamLocked(state)
	if err != nil {
		return nil, nil, err
	}
	if team.LeaderID != caller.SessionID {
		return nil, nil, fmt.Errorf("only the team leader can perform this action")
	}
	return caller, team, nil
}

func (m *Manager) ensureUniqueNameLocked(team *teamRecord, name string) error {
	if leader := m.participants[team.LeaderID]; leader != nil && leader.Name == name {
		return fmt.Errorf("AgentCreate: a team member named %q already exists", name)
	}
	for _, memberID := range team.MemberIDs {
		if member := m.participants[memberID]; member != nil && member.Name == name {
			return fmt.Errorf("AgentCreate: a team member named %q already exists", name)
		}
	}
	return nil
}

func (m *Manager) memberByNameLocked(team *teamRecord, name string) (*teamParticipant, bool) {
	if leader := m.participants[team.LeaderID]; leader != nil && leader.Name == name {
		return leader, true
	}
	for _, memberID := range team.MemberIDs {
		if member := m.participants[memberID]; member != nil && member.Name == name {
			return member, true
		}
	}
	return nil, false
}

func (m *Manager) snapshotLocked(teamID string) (*TeamSnapshot, bool) {
	team := m.teams[teamID]
	if team == nil {
		return nil, false
	}
	snapshot := &TeamSnapshot{
		ID:          team.ID,
		Name:        team.Name,
		Description: team.Description,
		Members:     []TeamMemberSnapshot{},
	}
	if leader := m.participants[team.LeaderID]; leader != nil {
		snapshot.Leader = leader.snapshot()
	}
	for _, memberID := range team.MemberIDs {
		if member := m.participants[memberID]; member != nil {
			snapshot.Members = append(snapshot.Members, member.snapshot())
		}
	}
	return snapshot, true
}

func (p *teamParticipant) snapshot() TeamMemberSnapshot {
	return TeamMemberSnapshot{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Role:        p.Role,
		SessionID:   p.SessionID,
	}
}

func (m *Manager) deliverLocked(sessionID, from, content string) {
	msg, err := newTeamMessage(from, content)
	if err != nil {
		return
	}
	m.inbox[sessionID] = append(m.inbox[sessionID], msg)
}

func newTeamMessage(from, content string) (*message.Message, error) {
	return message.NewUserMessage(from, fmt.Sprintf("<team-message from=%q>\n%s\n</team-message>", from, content))
}

func buildWorkerSystemPrompt(teamName, teamDescription, memberName, memberDescription string) string {
	sections := []string{fmt.Sprintf("You are %s, a member of team %q.", memberName, teamName)}
	if strings.TrimSpace(teamDescription) != "" {
		sections = append(sections, "Team purpose: "+teamDescription)
	}
	if strings.TrimSpace(memberDescription) != "" {
		sections = append(sections, "Your role: "+memberDescription)
	}
	sections = append(sections, "You communicate with the team leader and other members through the TeamSay tool. Other tool calls execute in your own session. Speak on the team only when you have something external to share; your private reasoning stays private.")
	return strings.Join(sections, "\n\n")
}

func (m *Manager) teamCreateTool() astool.Tool {
	tool, _ := astool.NewFunctionTool(
		"TeamCreate",
		"Create a new team led by your current session and return its team id. Use AgentCreate to add members, then TeamSay to coordinate them.",
		objectSchema(map[string]any{
			"name":        map[string]any{"type": "string", "description": "Display name of the team."},
			"description": map[string]any{"type": "string", "description": "Team purpose or shared context."},
		}, []any{"name", "description"}),
		func(_ context.Context, input map[string]any, state *asstate.AgentState) (message.ContentBlockList, error) {
			text, err := m.createTeam(state, stringInput(input, "name"), stringInput(input, "description"))
			return toolText(text, err), nil
		},
		teamToolOptions(false)...,
	)
	return tool
}

func (m *Manager) agentCreateTool() astool.Tool {
	tool, _ := astool.NewFunctionTool(
		"AgentCreate",
		"Add a new member to the team you lead. The prompt is delivered to the member immediately as its first team message.",
		objectSchema(map[string]any{
			"name":            map[string]any{"type": "string", "description": "Unique member name used by TeamSay(to)."},
			"description":     map[string]any{"type": "string", "description": "One-sentence member role."},
			"prompt":          map[string]any{"type": "string", "description": "Initial task delivered to the member."},
			"permission_mode": map[string]any{"type": "string", "enum": []any{"default", "accept_edits", "explore", "bypass", "dont_ask"}, "description": "Permission mode for the worker."},
		}, []any{"name", "description", "prompt"}),
		func(ctx context.Context, input map[string]any, state *asstate.AgentState) (message.ContentBlockList, error) {
			mode := permission.PermissionMode(stringInput(input, "permission_mode"))
			text, err := m.createAgent(ctx, state, stringInput(input, "name"), stringInput(input, "description"), stringInput(input, "prompt"), mode)
			return toolText(text, err), nil
		},
		teamToolOptions(false)...,
	)
	return tool
}

func (m *Manager) teamSayTool(role TeamRole) astool.Tool {
	description := "Send a message to a specific team member or broadcast to all members."
	if role == TeamRoleWorker {
		description = "Send a message to the team leader or broadcast to all team members. When you finish your assigned task, report results with this tool."
	}
	tool, _ := astool.NewFunctionTool(
		"TeamSay",
		description,
		objectSchema(map[string]any{
			"content": map[string]any{"type": "string", "description": "Message text."},
			"to":      map[string]any{"type": "string", "description": "Recipient member name. Omit to broadcast."},
		}, []any{"content"}),
		func(_ context.Context, input map[string]any, state *asstate.AgentState) (message.ContentBlockList, error) {
			text, err := m.say(state, stringInput(input, "content"), stringInput(input, "to"))
			return toolText(text, err), nil
		},
		teamToolOptions(true)...,
	)
	return tool
}

func (m *Manager) teamDeleteTool() astool.Tool {
	tool, _ := astool.NewFunctionTool(
		"TeamDelete",
		"Dissolve the team you currently lead and clean up all members.",
		objectSchema(nil, nil),
		func(_ context.Context, _ map[string]any, state *asstate.AgentState) (message.ContentBlockList, error) {
			text, err := m.deleteTeam(state)
			return toolText(text, err), nil
		},
		teamToolOptions(false)...,
	)
	return tool
}

func teamToolOptions(concurrencySafe bool) []astool.FunctionToolOption {
	return []astool.FunctionToolOption{
		astool.WithFunctionConcurrencySafe(concurrencySafe),
		astool.WithFunctionReadOnly(true),
		astool.WithFunctionStateInjected(true),
		astool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{
				Behavior:       permission.BehaviorAllow,
				Message:        "Team tool is allowed when attached to the agent.",
				DecisionReason: "Team tool attachment controls the caller role.",
			}, nil
		}),
	}
}

func objectSchema(properties map[string]any, required []any) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func stringInput(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, ok := input[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func toolText(text string, err error) message.ContentBlockList {
	if err != nil {
		text = err.Error()
	}
	return message.ContentBlockList{message.NewTextBlock(text)}
}

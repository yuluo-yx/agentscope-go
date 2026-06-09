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
	"fmt"
	"strings"
	"sync"

	agenterrors "github.com/yuluo-yx/agentscope-go/errors"
	"github.com/yuluo-yx/agentscope-go/message"
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
	Leader         *Agent
	Name           string
	Description    string
	Prompt         string
	SystemPrompt   string
	PermissionMode permission.PermissionMode
}

// TeamWorkerFactory creates a worker agent for AgentCreate.
type TeamWorkerFactory func(context.Context, TeamWorkerRequest) (*Agent, error)

// TeamManagerOption configures a TeamManager.
type TeamManagerOption func(*TeamManager)

// TeamManager stores process-local team membership and inbox messages.
type TeamManager struct {
	mu sync.Mutex

	workerFactory TeamWorkerFactory
	workerModel   ChatModel
	workerOptions []AgentOption

	participants map[string]*teamParticipant
	teams        map[string]*teamRecord
	sessionTeams map[string]string
	inbox        map[string][]*message.Message
}

type teamParticipant struct {
	ID          string
	Name        string
	Description string
	Role        TeamRole
	SessionID   string
	Agent       *Agent
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
func WithTeamWorkerFactory(factory TeamWorkerFactory) TeamManagerOption {
	return func(manager *TeamManager) {
		manager.workerFactory = factory
	}
}

// WithTeamWorkerModel sets the model used by the default worker factory.
func WithTeamWorkerModel(model ChatModel) TeamManagerOption {
	return func(manager *TeamManager) {
		manager.workerModel = model
	}
}

// WithTeamWorkerOptions sets options appended by the default worker factory.
func WithTeamWorkerOptions(opts ...AgentOption) TeamManagerOption {
	return func(manager *TeamManager) {
		manager.workerOptions = append([]AgentOption(nil), opts...)
	}
}

// NewTeamManager creates a process-local team manager.
func NewTeamManager(opts ...TeamManagerOption) *TeamManager {
	manager := &TeamManager{
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

// WithTeam registers an agent with a TeamManager and attaches team tools.
func WithTeam(manager *TeamManager, role TeamRole) AgentOption {
	return func(agent *Agent) error {
		if manager == nil {
			return agenterrors.NewDeveloperError("agent team manager is nil")
		}
		if err := manager.RegisterAgent(agent, role, ""); err != nil {
			return err
		}
		kit, err := manager.Toolkit(role)
		if err != nil {
			return err
		}
		agent.toolkit = composeToolProviders(agent.toolkit, kit)
		return nil
	}
}

func composeToolProviders(primary, secondary ToolProvider) ToolProvider {
	if primary == nil {
		return secondary
	}
	if secondary == nil {
		return primary
	}
	return compositeToolProvider{primary: primary, secondary: secondary}
}

// RegisterAgent makes an existing agent addressable by team tools.
func (m *TeamManager) RegisterAgent(agent *Agent, role TeamRole, description string) error {
	if m == nil {
		return agenterrors.NewDeveloperError("agent team manager is nil")
	}
	if agent == nil || agent.state == nil {
		return agenterrors.NewDeveloperError("team agent is nil")
	}
	if role == "" {
		role = TeamRoleLeader
	}
	sessionID := strings.TrimSpace(agent.state.SessionID)
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
	participant.Name = agent.AgentName()
	participant.Description = description
	participant.Role = role
	participant.SessionID = sessionID
	participant.Agent = agent
	return nil
}

// Toolkit returns the team tools visible to a role.
func (m *TeamManager) Toolkit(role TeamRole) (*astool.Toolkit, error) {
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
func (m *TeamManager) TeamForSession(sessionID string) (*TeamSnapshot, bool) {
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
func (m *TeamManager) Team(teamID string) (*TeamSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked(teamID)
}

// DrainInbox observes pending team messages on the agent and clears its inbox.
func (m *TeamManager) DrainInbox(ctx context.Context, agent *Agent) error {
	if agent == nil || agent.state == nil {
		return agenterrors.NewDeveloperError("team drain agent is nil")
	}
	messages := m.PendingMessages(agent.state.SessionID)
	if len(messages) == 0 {
		return nil
	}
	return agent.Observe(ctx, messages)
}

// PendingMessages returns and clears pending team messages for a session.
func (m *TeamManager) PendingMessages(sessionID string) []*message.Message {
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
func (m *TeamManager) PendingAgents() []*Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	agents := []*Agent{}
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

func (m *TeamManager) createTeam(state *asstate.AgentState, name, description string) (string, error) {
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

func (m *TeamManager) createAgent(ctx context.Context, state *asstate.AgentState, name, description, prompt string, mode permission.PermissionMode) (string, error) {
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
		Team:           *snapshot,
		Leader:         caller.Agent,
		Name:           name,
		Description:    description,
		Prompt:         prompt,
		SystemPrompt:   buildWorkerSystemPrompt(team.Name, team.Description, name, description),
		PermissionMode: mode,
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
	if worker == nil || worker.state == nil {
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
	participant := m.participants[worker.state.SessionID]
	participant.Name = name
	participant.Description = description
	participant.Role = TeamRoleWorker
	team.MemberIDs = append(team.MemberIDs, worker.state.SessionID)
	m.sessionTeams[worker.state.SessionID] = team.ID
	m.deliverLocked(worker.state.SessionID, caller.Name, prompt)
	return fmt.Sprintf("Member %q added to team %q.", name, team.Name), nil
}

func (m *TeamManager) say(state *asstate.AgentState, content, to string) (string, error) {
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

func (m *TeamManager) deleteTeam(state *asstate.AgentState) (string, error) {
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

func (m *TeamManager) defaultWorkerFactory(ctx context.Context, request TeamWorkerRequest) (*Agent, error) {
	if m.workerModel == nil {
		return nil, fmt.Errorf("AgentCreate: no worker factory or worker model configured")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := append([]AgentOption{}, m.workerOptions...)
	opts = append(opts, WithTeam(m, TeamRoleWorker))
	worker, err := NewAgent(request.Name, request.SystemPrompt, m.workerModel, opts...)
	if err != nil {
		return nil, err
	}
	if request.PermissionMode != "" && worker.state != nil {
		worker.state.PermissionContext = permission.NewContext(request.PermissionMode)
	}
	return worker, nil
}

func (m *TeamManager) callerLocked(state *asstate.AgentState) (*teamParticipant, error) {
	if state == nil {
		return nil, fmt.Errorf("team tool requires agent state")
	}
	caller := m.participants[state.SessionID]
	if caller == nil {
		return nil, fmt.Errorf("team tool caller session %q is not registered", state.SessionID)
	}
	return caller, nil
}

func (m *TeamManager) currentTeamLocked(state *asstate.AgentState) (*teamParticipant, *teamRecord, error) {
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

func (m *TeamManager) leaderTeamLocked(state *asstate.AgentState) (*teamParticipant, *teamRecord, error) {
	caller, team, err := m.currentTeamLocked(state)
	if err != nil {
		return nil, nil, err
	}
	if team.LeaderID != caller.SessionID {
		return nil, nil, fmt.Errorf("only the team leader can perform this action")
	}
	return caller, team, nil
}

func (m *TeamManager) ensureUniqueNameLocked(team *teamRecord, name string) error {
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

func (m *TeamManager) memberByNameLocked(team *teamRecord, name string) (*teamParticipant, bool) {
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

func (m *TeamManager) snapshotLocked(teamID string) (*TeamSnapshot, bool) {
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

func (m *TeamManager) deliverLocked(sessionID, from, content string) {
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

func (m *TeamManager) teamCreateTool() astool.Tool {
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

func (m *TeamManager) agentCreateTool() astool.Tool {
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
			if mode == "" {
				mode = permission.ModeDefault
			}
			text, err := m.createAgent(ctx, state, stringInput(input, "name"), stringInput(input, "description"), stringInput(input, "prompt"), mode)
			return toolText(text, err), nil
		},
		teamToolOptions(false)...,
	)
	return tool
}

func (m *TeamManager) teamSayTool(role TeamRole) astool.Tool {
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

func (m *TeamManager) teamDeleteTool() astool.Tool {
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

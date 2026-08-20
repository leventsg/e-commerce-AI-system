package eino

import (
	"fmt"
	"sync"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

type agentRunState string

/*
状态流转：
created -> running
tool_running -> running
awaiting_confirmation -> running

running -> tool_running

tool_running -> awaiting_confirmation

running -> finalizing

finalizing -> completed

any -> failed
any -> aborted
*/
const (
	agentRunStateCreated              agentRunState = "Created"
	agentRunStateRunning              agentRunState = "Running"
	agentRunStateToolRunning          agentRunState = "ToolRunning"
	agentRunStateAwaitingConfirmation agentRunState = "AwaitingConfirmation"
	agentRunStateFinalizing           agentRunState = "Finalizing"
	agentRunStateCompleted            agentRunState = "Completed"
	agentRunStateFailed               agentRunState = "Failed"
	agentRunStateAborted              agentRunState = "Aborted"
)

type agentRunConfig struct {
	RunID              string
	ConversationID     string
	AssistantMessageID string
}

// AgentRunStateMachine centralizes the Eino agent run lifecycle.
type AgentRunStateMachine struct {
	mu                   sync.Mutex
	runID                string
	conversationID       string
	assistantMessageID   string
	state                agentRunState
	businessExecuted     bool
	confirmationRequired bool
	activeToolCount      int
}

func newAgentRunStateMachine(config agentRunConfig) *AgentRunStateMachine {
	return &AgentRunStateMachine{
		runID:              config.RunID,
		conversationID:     config.ConversationID,
		assistantMessageID: config.AssistantMessageID,
		state:              agentRunStateCreated,
	}
}

func (m *AgentRunStateMachine) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != agentRunStateCreated {
		return m.invalidTransitionLocked("Start")
	}
	m.state = agentRunStateRunning
	return nil
}

func (m *AgentRunStateMachine) State() agentRunState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *AgentRunStateMachine) BusinessExecuted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.businessExecuted
}

func (m *AgentRunStateMachine) AssistantMessageID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.assistantMessageIDLocked()
}

func (m *AgentRunStateMachine) OnModelDelta(content string) (domain.AgentEvent, error) {
	if content == "" {
		return domain.AgentEvent{}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != agentRunStateRunning {
		return domain.AgentEvent{}, m.invalidTransitionLocked("OnModelDelta")
	}
	return domain.AgentEvent{
		Type:           domain.EventAssistantDelta,
		ConversationID: m.conversationID,
		MessageID:      m.assistantMessageIDLocked(),
		Content:        content,
		Done:           false,
	}, nil
}

func (m *AgentRunStateMachine) OnThinkingDelta(content string) (domain.AgentEvent, error) {
	if content == "" {
		return domain.AgentEvent{}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != agentRunStateRunning && m.state != agentRunStateToolRunning {
		return domain.AgentEvent{}, m.invalidTransitionLocked("OnThinkingDelta")
	}
	return domain.AgentEvent{
		Type:           domain.EventAssistantThinkingDelta,
		ConversationID: m.conversationID,
		Content:        content,
		Done:           false,
	}, nil
}

func (m *AgentRunStateMachine) OnToolProgress(messageID, toolName, content, dataJSON string) (domain.AgentEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != agentRunStateRunning && m.state != agentRunStateToolRunning {
		return domain.AgentEvent{}, m.invalidTransitionLocked("OnToolProgress")
	}
	m.state = agentRunStateToolRunning
	m.activeToolCount++
	return domain.AgentEvent{
		Type:           domain.EventToolProgress,
		ConversationID: m.conversationID,
		MessageID:      messageID,
		Tool:           toolName,
		Status:         "running",
		Content:        content,
		DataJSON:       dataJSON,
		Done:           false,
	}, nil
}

func (m *AgentRunStateMachine) OnToolResult(messageID, toolName, status, content, dataJSON string, businessExecuted bool) (domain.AgentEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != agentRunStateToolRunning {
		return domain.AgentEvent{}, m.invalidTransitionLocked("OnToolResult")
	}
	if m.activeToolCount > 0 {
		m.activeToolCount--
	}
	if m.activeToolCount == 0 {
		m.state = agentRunStateRunning
	}
	if businessExecuted {
		m.businessExecuted = true
	}
	return domain.AgentEvent{
		Type:             domain.EventToolResult,
		ConversationID:   m.conversationID,
		MessageID:        messageID,
		Tool:             toolName,
		Status:           status,
		Content:          content,
		DataJSON:         dataJSON,
		Done:             true,
		BusinessExecuted: businessExecuted,
	}, nil
}

func (m *AgentRunStateMachine) OnConfirmationRequired(confirmationID, toolName, summary string, expiresAt int64) (domain.AgentEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != agentRunStateToolRunning {
		return domain.AgentEvent{}, m.invalidTransitionLocked("OnConfirmationRequired")
	}
	m.state = agentRunStateAwaitingConfirmation
	m.confirmationRequired = true
	return domain.AgentEvent{
		Type:           domain.EventConfirmationRequired,
		ConversationID: m.conversationID,
		MessageID:      newAgentMessageID(),
		Tool:           toolName,
		ConfirmationID: confirmationID,
		Action:         toolName,
		Summary:        summary,
		Content:        summary,
		ExpiresAt:      expiresAt,
		Done:           true,
	}, nil
}

func (m *AgentRunStateMachine) Fail() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isTerminalLocked() {
		m.state = agentRunStateFailed
	}
}

func (m *AgentRunStateMachine) Abort() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isTerminalLocked() {
		m.state = agentRunStateAborted
	}
}

func (m *AgentRunStateMachine) assistantMessageIDLocked() string {
	if m.assistantMessageID == "" {
		m.assistantMessageID = newAgentMessageID()
	}
	return m.assistantMessageID
}

func (m *AgentRunStateMachine) invalidTransitionLocked(action string) error {
	return fmt.Errorf("agent run invalid transition: %s from %s", action, m.state)
}

func (m *AgentRunStateMachine) isTerminalLocked() bool {
	return m.state == agentRunStateAwaitingConfirmation || m.state == agentRunStateCompleted || m.state == agentRunStateFailed || m.state == agentRunStateAborted
}

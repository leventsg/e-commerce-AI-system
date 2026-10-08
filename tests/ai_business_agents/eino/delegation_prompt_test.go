//go:build ai_business_agents

package eino

import (
	"context"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"testing"
)

func TestDelegationInstructionsPreserveLiteralJSON(t *testing.T) {
	instruction := `Respond with {"status":"completed"}.`
	input := &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("task")}}
	messages, err := literalAgentModelInput(context.Background(), instruction, input)
	if err != nil || len(messages) != 2 || messages[0].Content != instruction || messages[1] != input.Messages[0] {
		t.Fatalf("literal instructions changed: %v %v", messages, err)
	}
}

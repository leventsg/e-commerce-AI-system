package eino

import (
	"context"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Session values hold trusted runtime state, not instruction template variables.
// Keep JSON examples literal when ADK creates or restores an agent's input.
func literalAgentModelInput(_ context.Context, instruction string, input *adk.AgentInput) ([]*schema.Message, error) {
	messages := make([]*schema.Message, 0, len(input.Messages)+1)
	if instruction != "" {
		messages = append(messages, schema.SystemMessage(instruction))
	}
	return append(messages, input.Messages...), nil
}

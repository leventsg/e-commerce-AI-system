package eino

import (
	"fmt"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func ConvertContextMessages(contextMessages []domain.ContextMessage) ([]*schema.Message, error) {
	messages := make([]*schema.Message, 0, len(contextMessages))
	for _, item := range contextMessages {
		switch item.Role {
		case domain.ContextRoleSystem:
			messages = append(messages, schema.SystemMessage(item.Content))
		case domain.ContextRoleUser:
			messages = append(messages, schema.UserMessage(item.Content))
		case domain.ContextRoleAssistant:
			messages = append(messages, schema.AssistantMessage(item.Content, nil))
		case domain.ContextRoleTool:
			messages = append(messages, schema.ToolMessage(item.Content, item.ToolCallID, schema.WithToolName(item.ToolName)))
		default:
			return nil, fmt.Errorf("unsupported context message role %q", item.Role)
		}
	}
	return messages, nil
}

package eino

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
)

type messageMetadata struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
}

func ConvertMessages(history []*aimessages.AiMessages) ([]*schema.Message, error) {
	messages := make([]*schema.Message, 0, len(history))
	for _, item := range history {
		if item == nil {
			continue
		}
		switch item.Role {
		case "user":
			messages = append(messages, schema.UserMessage(item.Content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(item.Content, nil))
		case "tool":
			meta, err := parseMessageMetadata(item.Metadata)
			if err != nil {
				return nil, err
			}
			messages = append(messages, schema.ToolMessage(item.Content, meta.ToolCallID, schema.WithToolName(meta.ToolName)))
		default:
			return nil, fmt.Errorf("unsupported ai message role %q", item.Role)
		}
	}
	return messages, nil
}

func parseMessageMetadata(metadata sql.NullString) (messageMetadata, error) {
	if !metadata.Valid || metadata.String == "" {
		return messageMetadata{}, nil
	}
	var meta messageMetadata
	if err := json.Unmarshal([]byte(metadata.String), &meta); err != nil {
		return messageMetadata{}, fmt.Errorf("parse message metadata: %w", err)
	}
	return meta, nil
}

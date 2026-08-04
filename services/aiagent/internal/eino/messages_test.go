package eino

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestConvertContextMessagesPreservesRolesAndToolMetadata(t *testing.T) {
	converted, err := ConvertContextMessages([]domain.ContextMessage{
		{Role: domain.ContextRoleSystem, Content: "system"},
		{Role: domain.ContextRoleUser, Content: "user"},
		{Role: domain.ContextRoleAssistant, Content: "assistant"},
		{Role: domain.ContextRoleTool, Content: `{"ok":true}`, ToolCallID: "call-1", ToolName: "cart_list"},
	})
	if err != nil {
		t.Fatalf("ConvertContextMessages() error = %v", err)
	}
	wantRoles := []schema.RoleType{schema.System, schema.User, schema.Assistant, schema.Tool}
	for i, want := range wantRoles {
		if converted[i].Role != want {
			t.Fatalf("message %d role = %q, want %q", i, converted[i].Role, want)
		}
	}
	if converted[3].ToolCallID != "call-1" || converted[3].ToolName != "cart_list" {
		t.Fatalf("tool message = %+v", converted[3])
	}
}

func TestConvertContextMessagesRejectsUnsupportedRole(t *testing.T) {
	if _, err := ConvertContextMessages([]domain.ContextMessage{{Role: "developer", Content: "bad"}}); err == nil {
		t.Fatal("ConvertContextMessages() error = nil")
	}
}

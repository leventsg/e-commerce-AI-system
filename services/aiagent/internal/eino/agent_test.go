package eino

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

type emptyResponseModel struct{ response *schema.Message }

func (m emptyResponseModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return m.response, nil
}
func (m emptyResponseModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

type capturingChatModel struct {
	response *schema.Message
	messages []*schema.Message
}

func (m *capturingChatModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.messages = messages
	return m.response, nil
}

func (m *capturingChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

type capturingToolCallingChatModel struct {
	capturingChatModel
	responses  []*schema.Message
	boundTools []*schema.ToolInfo
}

func (m *capturingToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.boundTools = tools
	return m, nil
}

func (m *capturingToolCallingChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.messages = append([]*schema.Message(nil), messages...)
	options := model.GetCommonOptions(&model.Options{}, opts...)
	if options.Tools != nil {
		m.boundTools = options.Tools
	}
	if len(m.responses) > 0 {
		response := m.responses[0]
		m.responses = m.responses[1:]
		return response, nil
	}
	return m.capturingChatModel.Generate(ctx, messages, opts...)
}

func (m *capturingToolCallingChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

type capturingInvokableTool struct {
	info      *schema.ToolInfo
	result    string
	calls     int
	arguments string
	execution any
}

func (t *capturingInvokableTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *capturingInvokableTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	t.calls++
	t.arguments = arguments
	execution, _ := aitools.ToolExecutionFromContext(ctx)
	t.execution = execution
	return t.result, nil
}

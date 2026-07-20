package eino

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type emptyResponseModel struct{ response *schema.Message }

func (m emptyResponseModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return m.response, nil
}
func (m emptyResponseModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

func TestRunnerRejectsNilAndEmptyModelResponses(t *testing.T) {
	for _, response := range []*schema.Message{nil, schema.AssistantMessage("", nil)} {
		_, err := NewRunner(emptyResponseModel{response: response}).Run(context.Background(), RunRequest{ConversationID: "conv-1", UserMessage: "hello"})
		if !errors.Is(err, ErrEmptyModelResponse) {
			t.Fatalf("Run() error = %v, want ErrEmptyModelResponse", err)
		}
	}
}

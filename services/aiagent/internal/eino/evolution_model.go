package eino

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
)

// ChatOnce 执行一次无工具绑定的模型调用并返回文本。用于 Self-Evolving 的独立
// 分析、候选生成与裁判阶段：这些调用与执行 Agent、候选生成之间上下文与 prompt
// 相互隔离。这里只封装 Eino 模型调用，具体 prompt 与 JSON 编排由调用方负责。
func ChatOnce(ctx context.Context, factory ModelFactory, cfg config.EinoConfig, system, user string) (string, error) {
	if factory == nil {
		return "", ErrModelUnavailable
	}
	chatModel, err := factory.NewChatModel(ctx, cfg)
	if err != nil {
		return "", err
	}
	response, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(user),
	})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if response == nil {
		return "", fmt.Errorf("%w: empty model response", ErrModelUnavailable)
	}
	return strings.TrimSpace(response.Content), nil
}

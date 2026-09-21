package eino

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/skills"
)

// agentSkillBindings 声明每个 Agent 绑定的 Skill：
// - issue-triage 面向含糊异常诊断，绑定 Supervisor；
// - purchase-advisor 面向选购咨询，绑定商品 Agent。
var agentSkillBindings = map[string][]string{
	supervisorAgentName: {skills.IssueTriage},
	"product_agent":     {skills.PurchaseAdvisor},
}

// skillBackend 将内嵌 SKILL.md 适配为 Eino 官方 skill.Backend，
// 使 Skill 中间件以渐进式披露方式按需加载策略全文（默认 inline 模式）。
type skillBackend struct {
	documents []skills.Document
}

func (b *skillBackend) List(context.Context) ([]skill.FrontMatter, error) {
	matters := make([]skill.FrontMatter, 0, len(b.documents))
	for _, document := range b.documents {
		matters = append(matters, skill.FrontMatter{
			Name:        document.Name,
			Description: document.Description,
		})
	}
	return matters, nil
}

func (b *skillBackend) Get(_ context.Context, name string) (skill.Skill, error) {
	for _, document := range b.documents {
		if document.Name == name {
			return skill.Skill{
				FrontMatter: skill.FrontMatter{
					Name:        document.Name,
					Description: document.Description,
				},
				Content: document.Content,
			}, nil
		}
	}
	return skill.Skill{}, fmt.Errorf("skill not found: %s", name)
}

// newSkillMiddleware 返回绑定到指定 Agent 的官方 Skill 中间件。
// 未绑定 Skill 的 Agent 返回 nil，保持原有 Handler 链不变。
func newSkillMiddleware(ctx context.Context, agentName string) (adk.ChatModelAgentMiddleware, error) {
	names := agentSkillBindings[agentName]
	if len(names) == 0 {
		return nil, nil
	}
	documents := make([]skills.Document, 0, len(names))
	for _, name := range names {
		document, ok := skills.ByName(name)
		if !ok {
			return nil, fmt.Errorf("ai skill %q not found for agent %s", name, agentName)
		}
		documents = append(documents, document)
	}
	return skill.NewMiddleware(ctx, &skill.Config{
		Backend: &skillBackend{documents: documents},
	})
}

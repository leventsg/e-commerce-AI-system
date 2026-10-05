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
// - purchase-advisor 面向选购咨询，绑定购物决策 Agent。
// 该映射是进程级默认值，评测 trial 的 SkillSet 覆盖只在单次 trial 内生效，
// 不修改此全局绑定变量。
var agentSkillBindings = map[string][]string{
	supervisorAgentName:       {skills.IssueTriage},
	"shopping_decision_agent": {skills.PurchaseAdvisor},
}

// SkillLoad 记录一次真实 Skill backend Get 调用：模型主动加载了哪个 Skill，
// 以及其内容哈希。它只反映实际加载，不强制加载正文。
type SkillLoad struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// SkillSet 描述单次评测 trial 的 Skill 覆盖，用于候选加载（修改/新增）而
// 不改变进程级绑定。
//
//   - Bindings 为空时使用内嵌默认绑定；非空时按 Agent 名覆盖整个绑定列表，
//     未出现的 Agent 回退到默认绑定。
//   - Documents 提供覆盖或新增的 Skill 正文，按 name 匹配；未命中的名称
//     回退到同名内嵌 Skill。
type SkillSet struct {
	Bindings  map[string][]string
	Documents map[string]skills.Document
}

// skillOverride 是 newSkillMiddleware 的内部参数：解析覆盖后的 Skill 集合，
// 并在真实 Get 调用时通过 onGet 记录加载证据。
type skillOverride struct {
	set   SkillSet
	onGet func(name, hash string)
}

// skillBackend 将 Skill 文档适配为 Eino 官方 skill.Backend，使 Skill 中间件
// 以渐进式披露方式按需加载策略全文（默认 inline 模式）。
type skillBackend struct {
	documents []skills.Document
	onGet     func(name, hash string)
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
			if b.onGet != nil {
				b.onGet(document.Name, skills.Hash(document))
			}
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
// override 提供 trial 级 Skill 覆盖；为 nil 时使用内嵌默认绑定与正文。
// 未绑定 Skill 的 Agent 返回 nil，保持原有 Handler 链不变。
func newSkillMiddleware(ctx context.Context, agentName string, override *skillOverride) (adk.ChatModelAgentMiddleware, error) {
	names := agentSkillBindings[agentName]
	onGet := func(string, string) {}
	if override != nil {
		if bound, ok := override.set.Bindings[agentName]; ok {
			names = bound
		}
		if override.onGet != nil {
			onGet = override.onGet
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	documents := make([]skills.Document, 0, len(names))
	for _, name := range names {
		document, ok := skills.ByName(name)
		if override != nil {
			if replaced, found := override.set.Documents[name]; found {
				document, ok = replaced, true
			}
		}
		if !ok {
			return nil, fmt.Errorf("ai skill %q not found for agent %s", name, agentName)
		}
		documents = append(documents, document)
	}
	return skill.NewMiddleware(ctx, &skill.Config{
		Backend: &skillBackend{documents: documents, onGet: onGet},
	})
}

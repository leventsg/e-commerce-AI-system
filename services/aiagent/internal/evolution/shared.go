package evolution

import (
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

// ABConfig 配置影子执行/重放校验使用的被测模型。
type ABConfig struct {
	TestedFactory eino.ModelFactory
	TestedConfig  config.EinoConfig
}

// writeTools 是所有写操作业务工具的全局集合，用于识别一次运行是否触发了写操作。
var writeTools = func() map[string]bool {
	set := map[string]bool{}
	for _, t := range tools.DefaultBusinessTools(tools.DefaultToolClients{}, config.ToolTimeoutConfig{}) {
		if t.Metadata.WriteOperation {
			set[t.Name] = true
		}
	}
	return set
}()

// stripFences 去掉模型输出可能包裹的 markdown 代码块围栏。
func stripFences(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

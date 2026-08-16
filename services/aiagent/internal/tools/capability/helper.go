package capability_tools

import (
	"math"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	aimemory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
)

type Deps struct {
	MemoryProvider aimemory.MemoryProvider
	ToolCallStore  *contextmanager.ToolCallStore
}

func BuildTools(deps Deps) []core.Tool {
	result := make([]core.Tool, 0, 2)
	// 如果 MemoryProvider 实现了 UserMemoryEventSearcher 接口，则添加搜索用户记忆的工具
	if searcher, ok := deps.MemoryProvider.(aimemory.UserMemoryEventSearcher); ok {
		result = append(result, newSearchUserMemoryTool(searcher))
	}
	// 如果 ToolCallStore 不为 nil，则添加获取工具调用结果的工具
	if deps.ToolCallStore != nil {
		result = append(result, newGetToolCallResultTool(deps.ToolCallStore))
	}
	return result
}

func stringSliceArgument(args map[string]any, name string) []string {
	raw, ok := args[name]
	if !ok || raw == nil {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func intNumberArgument(args map[string]any, name string) int {
	raw, ok := args[name]
	if !ok || raw == nil {
		return 0
	}
	switch value := raw.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0
		}
		return int(value)
	default:
		return 0
	}
}

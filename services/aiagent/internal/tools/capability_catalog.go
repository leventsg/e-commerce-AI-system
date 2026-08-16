package tools

import (
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	aimemory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
	capability_tools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/capability"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
)

type CapabilityDeps struct {
	MemoryProvider aimemory.MemoryProvider
	ToolCallStore  *contextmanager.ToolCallStore
}

func DefaultCapabilityTools(deps CapabilityDeps) []core.Tool {
	return capability_tools.BuildTools(capability_tools.Deps{
		MemoryProvider: deps.MemoryProvider,
		ToolCallStore:  deps.ToolCallStore,
	})
}

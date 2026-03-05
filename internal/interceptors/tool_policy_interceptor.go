package interceptors

import (
	"context"
	"fmt"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// ToolPolicyInterceptor applies deterministic allow/deny checks before tool execution.
type ToolPolicyInterceptor struct {
	BlockedTools    map[string]struct{}
	BlockedPrefixes []string
}

// NewToolPolicyInterceptor creates a policy interceptor with explicit denylist.
func NewToolPolicyInterceptor(blockedTools []string) core.ToolExecutionInterceptor {
	blocked := make(map[string]struct{}, len(blockedTools))
	for _, toolName := range blockedTools {
		key := strings.TrimSpace(toolName)
		if key == "" {
			continue
		}
		blocked[key] = struct{}{}
	}
	return &ToolPolicyInterceptor{
		BlockedTools: blocked,
		BlockedPrefixes: []string{
			".dangerous.",
		},
	}
}

func (i *ToolPolicyInterceptor) Name() string { return "ToolPolicyInterceptor" }

func (i *ToolPolicyInterceptor) AroundToolExecution(
	ctx context.Context,
	run *core.RunContext,
	call core.ToolCall,
	next core.ToolExecutionFunc,
) (core.ToolResult, error) {
	name := strings.TrimSpace(call.Name)
	if i != nil {
		if _, blocked := i.BlockedTools[name]; blocked {
			return core.ToolResult{}, fmt.Errorf("tool blocked by ToolPolicyInterceptor: %s", name)
		}
		for _, prefix := range i.BlockedPrefixes {
			if strings.HasPrefix(name, strings.TrimSpace(prefix)) {
				return core.ToolResult{}, fmt.Errorf("tool blocked by ToolPolicyInterceptor prefix rule: %s", name)
			}
		}
	}
	return next(ctx, run, call)
}

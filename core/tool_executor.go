package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolCall describes a requested tool invocation.
type ToolCall struct {
	Name      string
	Args      json.RawMessage
	Source    string
	Iteration int
}

// ToolExecutor centralizes tool execution control.
type ToolExecutor interface {
	Execute(ctx context.Context, run *RunContext, call ToolCall) (ToolResult, error)
}

// RegistryToolExecutor executes tools via ToolRegistry and interceptor chain.
type RegistryToolExecutor struct {
	registry     ToolRegistry
	interceptors *InterceptorRegistry
}

// NewRegistryToolExecutor creates a default tool executor on top of ToolRegistry.
func NewRegistryToolExecutor(registry ToolRegistry, interceptors *InterceptorRegistry) *RegistryToolExecutor {
	return &RegistryToolExecutor{
		registry:     registry,
		interceptors: interceptors,
	}
}

// Execute applies tool interceptors and delegates execution to ToolRegistry.
//
// Policy allow/deny checks, JSON schema validation, timeouts, retries, caching,
// deduplication and logging are enforced by the underlying registry.
func (e *RegistryToolExecutor) Execute(ctx context.Context, run *RunContext, call ToolCall) (ToolResult, error) {
	if e == nil || e.registry == nil {
		return ToolResult{}, fmt.Errorf("tool executor is not initialized")
	}
	call.Name = strings.TrimSpace(call.Name)
	if call.Name == "" {
		return ToolResult{}, fmt.Errorf("tool name is empty")
	}
	if call.Args == nil {
		call.Args = json.RawMessage("{}")
	}
	if run != nil && run.State != nil {
		if call.Iteration <= 0 {
			call.Iteration = run.State.Iteration
		}
	}

	handler := func(ctx context.Context, _ *RunContext, call ToolCall) (ToolResult, error) {
		return e.registry.Execute(ctx, call.Name, call.Args)
	}
	interceptors := e.toolInterceptors()
	for i := len(interceptors) - 1; i >= 0; i-- {
		interceptor := interceptors[i]
		next := handler
		handler = func(ctx context.Context, run *RunContext, call ToolCall) (ToolResult, error) {
			return interceptor.AroundToolExecution(ctx, run, call, next)
		}
	}

	startedAt := time.Now().UTC()
	result, err := handler(ctx, run, call)
	if run != nil {
		run.RecordToolExecution(ctx, call, result, err, time.Since(startedAt))
	}
	return result, err
}

func (e *RegistryToolExecutor) toolInterceptors() []ToolExecutionInterceptor {
	if e == nil || e.interceptors == nil {
		return nil
	}
	return e.interceptors.ToolInterceptors()
}

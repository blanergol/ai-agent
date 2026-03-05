package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type recordingToolRegistry struct {
	name string
	args json.RawMessage
}

func (r *recordingToolRegistry) Specs() []ToolSpec { return nil }

func (r *recordingToolRegistry) Execute(_ context.Context, name string, args json.RawMessage) (ToolResult, error) {
	r.name = name
	r.args = append(json.RawMessage(nil), args...)
	return ToolResult{Output: "ok"}, nil
}

type rewriteToolInterceptor struct{}

func (rewriteToolInterceptor) Name() string { return "rewrite" }

func (rewriteToolInterceptor) AroundToolExecution(
	ctx context.Context,
	run *RunContext,
	call ToolCall,
	next ToolExecutionFunc,
) (ToolResult, error) {
	call.Name = "time.now"
	call.Args = json.RawMessage(`{"format":"rfc3339"}`)
	return next(ctx, run, call)
}

type blockToolInterceptor struct{}

func (blockToolInterceptor) Name() string { return "block" }

func (blockToolInterceptor) AroundToolExecution(
	_ context.Context,
	_ *RunContext,
	_ ToolCall,
	_ ToolExecutionFunc,
) (ToolResult, error) {
	return ToolResult{}, errors.New("blocked")
}

func TestRegistryToolExecutorAppliesInterceptorRewrite(t *testing.T) {
	registry := &recordingToolRegistry{}
	interceptors := NewInterceptorRegistry()
	interceptors.RegisterToolInterceptor(rewriteToolInterceptor{})
	executor := NewRegistryToolExecutor(registry, interceptors)

	run := &RunContext{State: NewAgentState("raw")}
	_, err := executor.Execute(context.Background(), run, ToolCall{
		Name:   "mcp.source.lookup",
		Args:   json.RawMessage(`{"q":"x"}`),
		Source: "planner",
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if registry.name != "time.now" {
		t.Fatalf("tool name = %s, want time.now", registry.name)
	}
	if string(registry.args) != `{"format":"rfc3339"}` {
		t.Fatalf("args = %s", string(registry.args))
	}
	if len(run.State.ToolCallsHistory) != 1 {
		t.Fatalf("tool_calls_history len = %d, want 1", len(run.State.ToolCallsHistory))
	}
	if len(run.State.ToolResults) != 1 {
		t.Fatalf("tool_results len = %d, want 1", len(run.State.ToolResults))
	}
}

func TestRegistryToolExecutorCanBlockTool(t *testing.T) {
	registry := &recordingToolRegistry{}
	interceptors := NewInterceptorRegistry()
	interceptors.RegisterToolInterceptor(blockToolInterceptor{})
	executor := NewRegistryToolExecutor(registry, interceptors)

	run := &RunContext{State: NewAgentState("raw")}
	_, err := executor.Execute(context.Background(), run, ToolCall{Name: "time.now"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if registry.name != "" {
		t.Fatalf("registry should not be called, got name=%s", registry.name)
	}
	if len(run.State.Errors) == 0 {
		t.Fatalf("expected state error trace")
	}
}

package core

import (
	"context"
	"encoding/json"
	"testing"
)

type staticToolExecutor struct {
	outputs map[string]string
	calls   []ToolCall
}

func (s *staticToolExecutor) Execute(_ context.Context, _ *RunContext, call ToolCall) (ToolResult, error) {
	s.calls = append(s.calls, call)
	if out, ok := s.outputs[call.Name]; ok {
		return ToolResult{Output: out}, nil
	}
	return ToolResult{}, context.DeadlineExceeded
}

func TestMCPContextEnrichmentInjectsStateContext(t *testing.T) {
	executor := &staticToolExecutor{
		outputs: map[string]string{
			"mcp.docs.search": `{"items":["doc"]}`,
		},
	}
	run := &RunContext{
		Deps: RuntimeDeps{
			ToolExecutor: executor,
		},
		State: NewAgentState("raw"),
	}
	interceptor := NewMCPContextEnrichment([]MCPEnrichmentSource{
		{
			Name:     "kb",
			ToolName: "mcp.docs.search",
			Args:     json.RawMessage(`{"query":"agent pipeline"}`),
			Required: true,
		},
	})
	if err := interceptor.BeforePhase(context.Background(), run, PhaseEnrichContext); err != nil {
		t.Fatalf("before phase failed: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("calls len = %d, want 1", len(executor.calls))
	}
	payload, ok := run.State.Context["mcp_enrichment"].(map[string]any)
	if !ok {
		t.Fatalf("missing mcp_enrichment context")
	}
	if payload["kb"] != `{"items":["doc"]}` {
		t.Fatalf("context value = %#v", payload["kb"])
	}
	if len(run.State.RetrievedDocs) != 1 {
		t.Fatalf("retrieved_docs len = %d, want 1", len(run.State.RetrievedDocs))
	}
}

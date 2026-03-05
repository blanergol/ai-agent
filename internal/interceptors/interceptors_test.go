package interceptors

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/blanergol/agent-core/core"
)

type toolRegistryStub struct {
	specs []core.ToolSpec
}

func (s toolRegistryStub) Specs() []core.ToolSpec {
	out := make([]core.ToolSpec, len(s.specs))
	copy(out, s.specs)
	return out
}

func (s toolRegistryStub) Execute(_ context.Context, _ string, _ json.RawMessage) (core.ToolResult, error) {
	return core.ToolResult{}, nil
}

type stateSnapshotterStub struct {
	snapshot map[string]any
}

func (s stateSnapshotterStub) SnapshotForSession(_ context.Context) map[string]any {
	out := make(map[string]any, len(s.snapshot))
	for key, value := range s.snapshot {
		out[key] = value
	}
	return out
}

func TestContextEnrichmentInterceptorAddsIncidentContext(t *testing.T) {
	run := &core.RunContext{
		Input: core.RunInput{Text: "Critical outage, create incident for auth-gateway"},
		Deps: core.RuntimeDeps{
			State: stateSnapshotterStub{
				snapshot: map[string]any{"incident.last_id": "INC-000123"},
			},
			Tools: toolRegistryStub{
				specs: []core.ToolSpec{
					{Name: "mcp.obs.metrics"},
					{Name: ".incident.create"},
				},
			},
		},
	}
	interceptor := NewContextEnrichmentInterceptor()
	if err := interceptor.BeforePhase(context.Background(), run, core.PhaseEnrichContext); err != nil {
		t.Fatalf("before phase: %v", err)
	}
	contextData, ok := run.State.Context["IncidentContext"].(map[string]any)
	if !ok {
		t.Fatalf("incident context is missing")
	}
	if contextData["intent"] != "incident_creation" {
		t.Fatalf("intent = %v", contextData["intent"])
	}
	if contextData["severity_hint"] != "sev1" {
		t.Fatalf("severity_hint = %v", contextData["severity_hint"])
	}
	if len(run.State.RetrievedDocs) == 0 {
		t.Fatalf("retrieved docs should not be empty")
	}
}

func TestToolRewriteInterceptorAddsDefaults(t *testing.T) {
	interceptor := NewToolRewriteInterceptor()
	run := &core.RunContext{
		State: &core.AgentState{
			Context: map[string]any{
				"IncidentContext": map[string]any{
					"severity_hint": "sev2",
				},
			},
		},
	}

	var captured core.ToolCall
	_, err := interceptor.AroundToolExecution(
		context.Background(),
		run,
		core.ToolCall{
			Name: ".incident.create",
			Args: json.RawMessage(`{"service":"payments-api","summary":"Latency increased"}`),
		},
		func(_ context.Context, _ *core.RunContext, call core.ToolCall) (core.ToolResult, error) {
			captured = call
			return core.ToolResult{}, nil
		},
	)
	if err != nil {
		t.Fatalf("around execution: %v", err)
	}

	var args map[string]any
	if err := json.Unmarshal(captured.Args, &args); err != nil {
		t.Fatalf("decode rewritten args: %v", err)
	}
	if args["severity"] != "sev2" {
		t.Fatalf("severity = %v, want sev2", args["severity"])
	}
	if args["source"] != "internal.bundle" {
		t.Fatalf("source = %v, want internal.bundle", args["source"])
	}
}

func TestToolFallbackInterceptorServiceLookup(t *testing.T) {
	interceptor := NewToolFallbackInterceptor()
	result, err := interceptor.AroundToolExecution(
		context.Background(),
		&core.RunContext{},
		core.ToolCall{
			Name: ".service.lookup",
			Args: json.RawMessage(`{"service":"unknown-svc"}`),
		},
		func(_ context.Context, _ *core.RunContext, _ core.ToolCall) (core.ToolResult, error) {
			return core.ToolResult{}, fmt.Errorf("catalog unavailable")
		},
	)
	if err != nil {
		t.Fatalf("fallback should suppress error: %v", err)
	}
	if result.Output == "" {
		t.Fatalf("fallback output should not be empty")
	}
	if result.Metadata["fallback"] != true {
		t.Fatalf("fallback metadata missing: %#v", result.Metadata)
	}
}

func TestAfterToolExecutionStateInterceptorExtractsIncidentMemory(t *testing.T) {
	interceptor := NewAfterToolExecutionStateInterceptor()
	run := &core.RunContext{
		State: &core.AgentState{
			Context: map[string]any{},
			ToolResults: []core.ToolResultTrace{
				{
					ToolName: ".incident.create",
					Output:   `{"incident_id":"INC-000777","service":"auth-gateway","status":"investigating"}`,
				},
			},
		},
	}
	if err := interceptor.AfterPhase(context.Background(), run, core.PhaseAfterToolExecution, nil); err != nil {
		t.Fatalf("after phase: %v", err)
	}
	incidentMemory, ok := run.State.Context["IncidentMemory"].(map[string]any)
	if !ok {
		t.Fatalf("incident memory is missing")
	}
	if incidentMemory["incident_id"] != "INC-000777" {
		t.Fatalf("incident_id = %v", incidentMemory["incident_id"])
	}
}

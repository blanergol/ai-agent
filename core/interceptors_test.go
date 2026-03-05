package core

import (
	"context"
	"testing"
)

type phaseProbeInterceptor struct {
	name   string
	events *[]string
}

func (p phaseProbeInterceptor) Name() string { return p.name }

func (p phaseProbeInterceptor) BeforePhase(_ context.Context, _ *RunContext, _ Phase) error {
	if p.events != nil {
		*p.events = append(*p.events, "before:"+p.name)
	}
	return nil
}

func (p phaseProbeInterceptor) AfterPhase(_ context.Context, _ *RunContext, _ Phase, _ error) error {
	if p.events != nil {
		*p.events = append(*p.events, "after:"+p.name)
	}
	return nil
}

func TestExecutePhaseRunsInterceptorsInOrder(t *testing.T) {
	events := make([]string, 0, 6)
	registry := NewInterceptorRegistry()
	registry.RegisterInterceptor(PhaseEnrichContext, phaseProbeInterceptor{name: "a", events: &events})
	registry.RegisterInterceptor(PhaseEnrichContext, phaseProbeInterceptor{name: "b", events: &events})

	run := &RunContext{
		Deps:  RuntimeDeps{Interceptors: registry},
		State: NewAgentState("raw"),
	}
	err := run.ExecutePhase(context.Background(), PhaseEnrichContext, func(_ context.Context) error {
		events = append(events, "run")
		return nil
	})
	if err != nil {
		t.Fatalf("execute phase failed: %v", err)
	}
	want := []string{"before:a", "before:b", "run", "after:b", "after:a"}
	if len(events) != len(want) {
		t.Fatalf("events len = %d, want %d (%#v)", len(events), len(want), events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events[%d] = %s, want %s", i, events[i], want[i])
		}
	}
	if len(run.State.Trace.Phases) != 1 {
		t.Fatalf("phase traces = %d, want 1", len(run.State.Trace.Phases))
	}
	if run.State.Trace.Phases[0].Phase != PhaseEnrichContext {
		t.Fatalf("phase trace = %s, want %s", run.State.Trace.Phases[0].Phase, PhaseEnrichContext)
	}
}

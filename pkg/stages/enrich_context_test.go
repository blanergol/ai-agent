package stages

import (
	"context"
	"testing"

	"github.com/blanergol/agent-core/core"
)

type phaseMarkInterceptor struct {
	ran bool
}

func (p *phaseMarkInterceptor) Name() string { return "mark" }

func (p *phaseMarkInterceptor) BeforePhase(_ context.Context, _ *core.RunContext, phase core.Phase) error {
	if phase == core.PhaseEnrichContext {
		p.ran = true
	}
	return nil
}

func (p *phaseMarkInterceptor) AfterPhase(_ context.Context, _ *core.RunContext, _ core.Phase, _ error) error {
	return nil
}

func TestEnrichContextStageRunsPhase(t *testing.T) {
	interceptors := core.NewInterceptorRegistry()
	marker := &phaseMarkInterceptor{}
	interceptors.RegisterInterceptor(core.PhaseEnrichContext, marker)
	run := &core.RunContext{
		Deps: core.RuntimeDeps{
			Interceptors: interceptors,
		},
		State: core.NewAgentState("raw"),
	}
	stage := NewEnrichContextStage()
	if _, err := stage.Run(context.Background(), run); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !marker.ran {
		t.Fatalf("enrich interceptor did not run")
	}
}

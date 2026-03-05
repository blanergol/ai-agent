package stages

import (
	"context"

	"github.com/blanergol/agent-core/core"
)

// EnrichContextStage runs deterministic context enrichment hooks.
type EnrichContextStage struct{}

// NewEnrichContextStage creates ENRICH_CONTEXT phase stage.
func NewEnrichContextStage() core.Stage {
	return &EnrichContextStage{}
}

// Name returns stable stage identifier.
func (s *EnrichContextStage) Name() string { return "enrich_context" }

// Run executes deterministic enrichment phase.
func (s *EnrichContextStage) Run(ctx context.Context, run *core.RunContext) (core.StageResult, error) {
	if run.PendingStop {
		return core.Continue(), nil
	}
	if err := run.ExecutePhase(ctx, core.PhaseEnrichContext, nil); err != nil {
		return core.StageResult{}, err
	}
	return core.Continue(), nil
}

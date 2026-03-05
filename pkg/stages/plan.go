package stages

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/apperrors"
)

// PlanStage запрашивает у планировщика следующее действие и контролирует повторяющиеся шаги.
type PlanStage struct{}

// NewPlanStage создает стадию планирования.
func NewPlanStage() core.Stage {
	return &PlanStage{}
}

// Name возвращает стабильный идентификатор стадии планирования.
func (s *PlanStage) Name() string { return "plan" }

// Run получает NextAction, записывает плановый шаг и при необходимости инициирует остановку.
func (s *PlanStage) Run(ctx context.Context, run *core.RunContext) (core.StageResult, error) {
	if run.PendingStop {
		return core.Continue(), nil
	}
	var next core.NextAction
	if err := run.ExecutePhase(ctx, core.PhaseDecideAction, func(ctx context.Context) error {
		if run.State != nil {
			run.Observation.Context = stageCopyMap(run.State.Context)
			run.Observation.RetrievedDocs = stageCopyStrings(run.State.RetrievedDocs)
		}
		var lastErr error
		for attempt := 0; attempt <= run.Config.MaxPlanningRetries; attempt++ {
			candidate, err := run.Deps.Planner.Plan(ctx, run.Observation)
			if err == nil {
				next = candidate
				lastErr = nil
				break
			}
			lastErr = err
			if ctx.Err() != nil {
				break
			}
		}
		if lastErr != nil {
			return apperrors.Wrap(apperrors.CodeTransient, "planner failed after retries", lastErr, true)
		}
		return nil
	}); err != nil {
		return core.StageResult{}, err
	}
	run.NextAction = next
	run.PlanningSteps = append(run.PlanningSteps, core.PlanningStep{
		Step:             run.CurrentStep,
		ActionType:       strings.TrimSpace(next.Action.Type),
		ToolName:         strings.TrimSpace(next.Action.ToolName),
		ReasoningSummary: strings.TrimSpace(next.Action.ReasoningSummary),
		ExpectedOutcome:  strings.TrimSpace(next.Action.ExpectedOutcome),
		Done:             next.Done,
	})

	fingerprint := core.ActionFingerprint(next.Action)
	run.ActionRepeats[fingerprint]++
	if next.Action.Type == core.ActionTypeTool && run.ActionRepeats[fingerprint] > 1 {
		run.PendingStop = true
		final := strings.TrimSpace(run.ActionResult)
		if final != "" {
			run.PendingStopReason = "planner_done"
		} else {
			run.PendingStopReason = "repeated_action_detected"
		}
		if final == "" {
			final = core.FallbackFinalResponse(run.PendingStopReason)
		}
		run.PendingFinalResponse = final
		return core.Continue(), nil
	}
	if run.ActionRepeats[fingerprint] > 3 {
		run.PendingStop = true
		run.PendingStopReason = "repeated_action_detected"
		final := strings.TrimSpace(run.ActionResult)
		if final == "" {
			final = core.FallbackFinalResponse(run.PendingStopReason)
		}
		run.PendingFinalResponse = final
		return core.Continue(), nil
	}

	run.Notify(ctx, core.Event{
		Type:       core.EventStepPlanned,
		Step:       run.CurrentStep,
		ActionType: next.Action.Type,
		ToolName:   next.Action.ToolName,
	})
	return core.Continue(), nil
}

func stageCopyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func stageCopyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

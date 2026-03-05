package stages

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// ReflectStage анализирует результат действия и решает, продолжать цикл или переходить к остановке.
type ReflectStage struct{}

// NewReflectStage создает стадию рефлексии.
func NewReflectStage() core.Stage {
	return &ReflectStage{}
}

// Name возвращает стабильный идентификатор стадии рефлексии.
func (s *ReflectStage) Name() string { return "reflect" }

// Run вычисляет stop-condition на основе текущего результата действия и флагов planner-а.
func (s *ReflectStage) Run(ctx context.Context, run *core.RunContext) (core.StageResult, error) {
	if run.PendingStop {
		return core.Continue(), nil
	}
	stop := false
	reason := "continue"
	if err := run.ExecutePhase(ctx, core.PhaseEvaluate, func(_ context.Context) error {
		if run.ActionDone {
			stop = true
			reason = "planner_done"
		} else if run.NextAction.Done {
			stop = true
			reason = "stop_condition"
		} else if run.NextAction.Action.Type == core.ActionTypeTool && strings.TrimSpace(run.ActionResult) == "" {
			stop = false
			reason = "continue"
		}
		return nil
	}); err != nil {
		return core.StageResult{}, err
	}
	if err := run.ExecutePhase(ctx, core.PhaseStopCheck, func(_ context.Context) error {
		if !stop {
			return nil
		}
		run.PendingStop = true
		run.PendingStopReason = reason
		final := strings.TrimSpace(run.ActionResult)
		if final == "" {
			final = core.FallbackFinalResponse(reason)
		}
		run.PendingFinalResponse = final
		return nil
	}); err != nil {
		return core.StageResult{}, err
	}
	return core.Continue(), nil
}

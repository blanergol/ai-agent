package stages

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/redact"
)

// StopStage завершает запуск агента и валидирует финальный ответ перед возвратом наружу.
type StopStage struct{}

// NewStopStage создает стадию финализации run-цикла.
func NewStopStage() core.Stage {
	return &StopStage{}
}

// Name возвращает стабильный идентификатор стадии остановки.
func (s *StopStage) Name() string { return "stop" }

// Run валидирует финальный ответ и либо завершает запуск, либо инициирует повторную попытку.
func (s *StopStage) Run(ctx context.Context, run *core.RunContext) (core.StageResult, error) {
	if !run.PendingStop {
		return core.Continue(), nil
	}
	stageResult := core.Continue()
	if err := run.ExecutePhase(ctx, core.PhaseFinalize, func(ctx context.Context) error {
		final := strings.TrimSpace(run.PendingFinalResponse)
		if final == "" {
			final = core.FallbackFinalResponse(run.PendingStopReason)
		}
		if err := run.Deps.OutputValidator.Validate(ctx, final); err != nil {
			if run.OutputValidationAttempts < run.Config.OutputValidationRetries {
				run.OutputValidationAttempts++
				_ = run.Memory.AddSystemMessage(
					ctx,
					"Final response was rejected by output policy. Return a corrected safe response only.",
				)
				run.Notify(ctx, core.Event{
					Type:       core.EventOutputInvalid,
					Step:       run.CurrentStep,
					StopReason: redact.Error(err),
				})
				run.PendingStop = false
				run.PendingStopReason = ""
				run.PendingFinalResponse = ""
				stageResult = core.Retry("output_validation_retry")
				return nil
			}
			return err
		}
		run.FinalResponse = final
		run.StopReason = strings.TrimSpace(run.PendingStopReason)
		stageResult = core.Stop(run.StopReason)
		return nil
	}); err != nil {
		return core.StageResult{}, err
	}
	return stageResult, nil
}

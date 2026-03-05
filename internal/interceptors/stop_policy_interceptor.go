package interceptors

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// StopPolicyInterceptor применяет детерминированные бизнес-условия остановки.
type StopPolicyInterceptor struct {
	MaxIterations int
}

// NewStopPolicyInterceptor создаёт перехватчик политики остановки.
func NewStopPolicyInterceptor(MaxIterations int) core.PhaseInterceptor {
	if MaxIterations <= 0 {
		MaxIterations = 6
	}
	return &StopPolicyInterceptor{MaxIterations: MaxIterations}
}

// Name возвращает стабильное имя перехватчика.
func (i *StopPolicyInterceptor) Name() string { return "StopPolicyInterceptor" }

// BeforePhase выполняет детерминированные бизнес-проверки остановки.
func (i *StopPolicyInterceptor) BeforePhase(_ context.Context, run *core.RunContext, _ core.Phase) error {
	if run == nil || run.PendingStop {
		return nil
	}

	iteration := 0
	if run.State != nil {
		iteration = run.State.Iteration
	}

	forceStop := false
	if run.State != nil && run.State.Context != nil {
		if value, ok := run.State.Context["_force_stop"].(bool); ok {
			forceStop = value
		}
	}

	switch {
	case forceStop:
		run.PendingStop = true
		run.PendingStopReason = "_forced_stop"
	case iteration >= i.MaxIterations:
		run.PendingStop = true
		run.PendingStopReason = "_iteration_cap"
	default:
		return nil
	}

	if strings.TrimSpace(run.PendingFinalResponse) == "" {
		run.PendingFinalResponse = core.FallbackFinalResponse(run.PendingStopReason)
	}
	return nil
}

// AfterPhase ничего не делает для политики остановки.
func (i *StopPolicyInterceptor) AfterPhase(_ context.Context, _ *core.RunContext, _ core.Phase, _ error) error {
	return nil
}

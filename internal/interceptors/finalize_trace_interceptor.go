package interceptors

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// FinalizeTraceInterceptor фиксирует детерминированную диагностику финализации.
type FinalizeTraceInterceptor struct{}

// NewFinalizeTraceInterceptor создаёт перехватчик диагностики финализации.
func NewFinalizeTraceInterceptor() core.PhaseInterceptor {
	return &FinalizeTraceInterceptor{}
}

// Name возвращает стабильное имя перехватчика.
func (i *FinalizeTraceInterceptor) Name() string { return "FinalizeTraceInterceptor" }

// BeforePhase ничего не делает для диагностики финализации.
func (i *FinalizeTraceInterceptor) BeforePhase(_ context.Context, _ *core.RunContext, _ core.Phase) error {
	return nil
}

// AfterPhase сохраняет решение о финализации в контекст состояния.
func (i *FinalizeTraceInterceptor) AfterPhase(
	_ context.Context,
	run *core.RunContext,
	_ core.Phase,
	phaseErr error,
) error {
	if run == nil || run.State == nil {
		return nil
	}
	if run.State.Context == nil {
		run.State.Context = map[string]any{}
	}
	payload := map[string]any{
		"stop_reason": strings.TrimSpace(run.PendingStopReason),
		"final_ready": strings.TrimSpace(run.PendingFinalResponse) != "",
		"has_error":   phaseErr != nil,
	}
	if phaseErr != nil {
		payload["error"] = strings.TrimSpace(phaseErr.Error())
	}
	run.State.Context["FinalizeTrace"] = payload
	return nil
}

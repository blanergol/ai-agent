package interceptors

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// NormalizeTraceInterceptor собирает диагностику нормализации.
type NormalizeTraceInterceptor struct{}

// NewNormalizeTraceInterceptor создаёт перехватчик диагностики нормализации.
func NewNormalizeTraceInterceptor() core.PhaseInterceptor {
	return &NormalizeTraceInterceptor{}
}

// Name возвращает стабильное имя перехватчика.
func (i *NormalizeTraceInterceptor) Name() string { return "NormalizeTraceInterceptor" }

// BeforePhase фиксирует состояние входа до этапа sanitize/normalize.
func (i *NormalizeTraceInterceptor) BeforePhase(_ context.Context, run *core.RunContext, _ core.Phase) error {
	if run == nil {
		return nil
	}
	if run.State == nil {
		run.State = core.NewAgentState(run.Input.Text)
	}
	if run.State.Context == nil {
		run.State.Context = map[string]any{}
	}
	run.State.Context["NormalizeTraceBefore"] = map[string]any{
		"input_chars": len(strings.TrimSpace(run.Input.Text)),
	}
	return nil
}

// AfterPhase фиксирует диагностику результата нормализации.
func (i *NormalizeTraceInterceptor) AfterPhase(
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
		"normalized_chars": len(strings.TrimSpace(run.Input.Text)),
		"has_error":        phaseErr != nil,
	}
	if phaseErr != nil {
		payload["error"] = strings.TrimSpace(phaseErr.Error())
	}
	run.State.Context["NormalizeTraceAfter"] = payload
	return nil
}

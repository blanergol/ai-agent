package pipeline

import (
	"context"
	"strings"

	"github.com/blanergol/agent-core/core"
)

// PostReflectAuditStage фиксирует состояние детерминированного бизнес-аудита.
type PostReflectAuditStage struct{}

// NewPostReflectAuditStage создаёт этап после reflect.
func NewPostReflectAuditStage() core.Stage {
	return &PostReflectAuditStage{}
}

// Name возвращает стабильный идентификатор этапа.
func (s *PostReflectAuditStage) Name() string { return "_post_reflect_audit" }

// Run сохраняет маркеры аудита по итерации и может финализировать ожидающий ответ.
func (s *PostReflectAuditStage) Run(_ context.Context, run *core.RunContext) (core.StageResult, error) {
	if run == nil {
		return core.Continue(), nil
	}
	if run.State != nil && run.State.Context == nil {
		run.State.Context = map[string]any{}
	}

	steps, toolCalls := 0, 0
	if run.Guardrails != nil {
		steps, toolCalls, _ = run.Guardrails.Stats()
	}
	if run.State != nil {
		run.State.Context["PostReflectAudit"] = map[string]any{
			"steps":        steps,
			"tool_calls":   toolCalls,
			"pending_stop": run.PendingStop,
			"stop_reason":  strings.TrimSpace(run.PendingStopReason),
		}
	}

	if run.PendingStop && strings.TrimSpace(run.PendingFinalResponse) == "" {
		run.PendingFinalResponse = core.FallbackFinalResponse(run.PendingStopReason)
	}
	return core.Continue(), nil
}

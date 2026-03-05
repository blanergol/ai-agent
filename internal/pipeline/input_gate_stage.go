package pipeline

import (
	"context"
	"strings"
	"time"

	"github.com/blanergol/agent-core/core"
)

// InputGateStage блокирует явно опасные фразы до этапа планирования.
type InputGateStage struct {
	BlockedPhrases []string
}

// NewInputGateStage создаёт детерминированный этап входного контроля.
func NewInputGateStage() core.Stage {
	return &InputGateStage{
		BlockedPhrases: []string{
			"drop table",
			"rm -rf /",
			"ignore all safeguards",
			"delete all incidents",
			"disable incident policy",
		},
	}
}

// Name возвращает стабильный идентификатор этапа.
func (s *InputGateStage) Name() string { return "_input_gate" }

// Run применяет детерминированную бизнес-политику входа до этапов observe/planner.
func (s *InputGateStage) Run(_ context.Context, run *core.RunContext) (core.StageResult, error) {
	if run == nil || run.PendingStop {
		return core.Continue(), nil
	}

	if run.State != nil && run.State.Context == nil {
		run.State.Context = map[string]any{}
	}

	inputLower := strings.ToLower(strings.TrimSpace(run.Input.Text))
	blockedPhrase := ""
	for _, phrase := range s.BlockedPhrases {
		if phrase == "" {
			continue
		}
		if strings.Contains(inputLower, phrase) {
			blockedPhrase = phrase
			break
		}
	}

	if run.State != nil {
		run.State.Context["InputGate"] = map[string]any{
			"checked_at":     time.Now().UTC().Format(time.RFC3339),
			"blocked":        blockedPhrase != "",
			"blocked_phrase": blockedPhrase,
		}
	}

	if blockedPhrase == "" {
		return core.Continue(), nil
	}

	run.PendingStop = true
	run.PendingStopReason = "_input_gate_blocked"
	run.PendingFinalResponse = "Request blocked by InputGateStage safety policy."
	if run.State != nil {
		run.State.Errors = append(run.State.Errors, "InputGate blocked phrase: "+blockedPhrase)
	}
	return core.Continue(), nil
}

package guardrails

import (
	"testing"
	"time"

	"github.com/blanergol/agent-core/core"
)

// TestValidateActionBlocksToolOutsideAllowlist проверяет блокировку инструмента вне allowlist.
func TestValidateActionBlocksToolOutsideAllowlist(t *testing.T) {
	g := New(Config{ToolAllowlist: []string{"time.now"}})

	err := g.ValidateAction(core.Action{Type: core.ActionTypeTool, ToolName: "http.get", ReasoningSummary: "x", ExpectedOutcome: "y"})
	if err == nil {
		t.Fatalf("expected error for blocked tool")
	}
}

// TestBeforeStepHonorsMaxSteps проверяет ограничение максимального числа шагов.
func TestBeforeStepHonorsMaxSteps(t *testing.T) {
	g := New(Config{MaxSteps: 1, MaxDuration: time.Minute})

	if err := g.BeforeStep(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := g.BeforeStep(); err == nil {
		t.Fatalf("expected max steps error")
	}
}

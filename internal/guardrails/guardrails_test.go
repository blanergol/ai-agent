package guardrails

import (
	"testing"
	"time"

	"github.com/blanergol/agent-core/internal/planner"
)

// TestValidateActionBlocksToolOutsideAllowlist проверяет, что инструмент вне allowlist блокируется.
func TestValidateActionBlocksToolOutsideAllowlist(t *testing.T) {
	// g настраивается с явным списком разрешённых инструментов.
	g := New(Config{ToolAllowlist: []string{"time.now"}})

	err := g.ValidateAction(planner.Action{Type: "tool", ToolName: "http.get", ReasoningSummary: "x", ExpectedOutcome: "y"})
	if err == nil {
		t.Fatalf("expected error for blocked tool")
	}
}

// TestBeforeStepHonorsMaxSteps проверяет срабатывание лимита шагов.
func TestBeforeStepHonorsMaxSteps(t *testing.T) {
	// g допускает ровно один шаг.
	g := New(Config{MaxSteps: 1, MaxDuration: time.Minute})

	if err := g.BeforeStep(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := g.BeforeStep(); err == nil {
		t.Fatalf("expected max steps error")
	}
}

package stages

import (
	"context"
	"strings"
	"testing"

	"github.com/blanergol/agent-core/core"
)

// upperSanitizer — тестовая реализация Sanitizer, нормализующая текст в верхний регистр.
type upperSanitizer struct{}

// Sanitize обрезает пробелы и переводит текст в верхний регистр.
func (upperSanitizer) Sanitize(text string) (string, error) {
	return strings.ToUpper(strings.TrimSpace(text)), nil
}

// TestSanitizeStage проверяет применение нормализации к пользовательскому вводу.
func TestSanitizeStage(t *testing.T) {
	stage := NewSanitizeStage(upperSanitizer{})
	run := &core.RunContext{
		Input: core.RunInput{Text: "  hello world  "},
	}
	result, err := stage.Run(context.Background(), run)
	if err != nil {
		t.Fatalf("sanitize run failed: %v", err)
	}
	if result.Control != core.StageControlContinue {
		t.Fatalf("control = %s", result.Control)
	}
	if run.Input.Text != "HELLO WORLD" {
		t.Fatalf("input = %q", run.Input.Text)
	}
}

// TestBuildPipelineWithSanitizeBeforePlan проверяет сборку пайплайна с явной стадией sanitize.
func TestBuildPipelineWithSanitizeBeforePlan(t *testing.T) {
	registry := NewRegistry()
	pipeline, err := registry.Build(
		[]string{"observe", "sanitize", "plan", "act", "reflect", "stop"},
		FactoryConfig{Sanitizer: upperSanitizer{}},
	)
	if err != nil {
		t.Fatalf("build pipeline failed: %v", err)
	}
	names := pipeline.StageNames()
	want := []string{"observe", "sanitize", "plan", "act", "reflect", "stop"}
	if len(names) != len(want) {
		t.Fatalf("len(names) = %d, want %d", len(names), len(want))
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names[%d] = %s, want %s", i, names[i], want[i])
		}
	}
}

package stages

import (
	"context"
	"testing"

	"github.com/blanergol/agent-core/core"
)

// stubStage — минимальная стадия-заглушка для проверки мутаций пайплайна.
type stubStage struct {
	name string
}

// Name возвращает идентификатор тестовой стадии.
func (s stubStage) Name() string { return s.name }

// Run завершает выполнение без изменений run-контекста.
func (s stubStage) Run(_ context.Context, _ *core.RunContext) (core.StageResult, error) {
	return core.Continue(), nil
}

// TestBuildDefaultPipelineAppliesMutations проверяет корректное применение мутаций к базовому пайплайну.
func TestBuildDefaultPipelineAppliesMutations(t *testing.T) {
	pipeline, err := BuildDefaultPipeline(
		FactoryConfig{},
		InsertBefore("plan", stubStage{name: "biz.sanitize"}),
		Append(stubStage{name: "biz.audit"}),
	)
	if err != nil {
		t.Fatalf("build pipeline: %v", err)
	}
	got := pipeline.StageNames()
	want := []string{"observe", "enrich_context", "biz.sanitize", "plan", "act", "reflect", "stop", "biz.audit"}
	if len(got) != len(want) {
		t.Fatalf("len(names) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestBuildDefaultPipelineMutationError проверяет возврат ошибки при невалидной мутации.
func TestBuildDefaultPipelineMutationError(t *testing.T) {
	_, err := BuildDefaultPipeline(FactoryConfig{}, Remove("missing-stage"))
	if err == nil {
		t.Fatalf("expected mutation error")
	}
}

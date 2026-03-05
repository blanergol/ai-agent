package skills

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/stages"
	"github.com/blanergol/agent-core/pkg/tools"
)

// testTool — минимальный инструмент-заглушка для проверки регистрации навыка.
type testTool struct{}

// Name возвращает идентификатор тестового инструмента.
func (testTool) Name() string { return "demo.tool" }

// Description возвращает краткое описание тестового инструмента.
func (testTool) Description() string { return "demo" }

// InputSchema возвращает схему входных аргументов тестового инструмента.
func (testTool) InputSchema() string { return `{"type":"object"}` }

// OutputSchema возвращает схему результата тестового инструмента.
func (testTool) OutputSchema() string {
	return `{"type":"string"}`
}

// Execute возвращает фиксированный результат и не выполняет побочных действий.
func (testTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Output: "ok"}, nil
}

// testSkill — тестовый навык, добавляющий инструмент, prompt и мутацию пайплайна.
type testSkill struct{}

// Name возвращает имя тестового навыка.
func (testSkill) Name() string { return "demo" }

// Register регистрирует тестовый инструмент в реестре.
func (testSkill) Register(registry *tools.Registry) error {
	return registry.Register(testTool{})
}

// PromptAdditions возвращает дополнительную инструкцию от тестового навыка.
func (testSkill) PromptAdditions() []string { return []string{"demo prompt"} }

// PipelineMutations вставляет тестовую стадию перед планированием.
func (testSkill) PipelineMutations() []stages.PipelineMutation {
	return []stages.PipelineMutation{stages.InsertBefore("plan", skillStage{name: "skill.guard"})}
}

// skillStage — простая стадия-заглушка, используемая в тестах расширения пайплайна.
type skillStage struct{ name string }

// Name возвращает имя стадии-заглушки.
func (s skillStage) Name() string { return s.name }

// Run завершает выполнение без изменения состояния.
func (s skillStage) Run(_ context.Context, _ *core.RunContext) (core.StageResult, error) {
	return core.Continue(), nil
}

// TestRegistryApplyIncludesPipelineExtensions проверяет полный эффект применения навыка.
func TestRegistryApplyIncludesPipelineExtensions(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(testSkill{}); err != nil {
		t.Fatalf("register skill: %v", err)
	}
	toolRegistry := tools.NewRegistry(tools.RegistryConfig{}, slog.Default())
	result, err := r.Apply([]string{"demo"}, toolRegistry)
	if err != nil {
		t.Fatalf("apply skills: %v", err)
	}
	if len(result.PromptAdditions) != 1 || result.PromptAdditions[0] != "demo prompt" {
		t.Fatalf("unexpected prompts: %#v", result.PromptAdditions)
	}
	if len(result.Pipeline) != 1 {
		t.Fatalf("pipeline mutations len = %d, want 1", len(result.Pipeline))
	}
	specs := toolRegistry.Specs()
	if len(specs) != 1 || specs[0].Name != "demo.tool" {
		t.Fatalf("unexpected tool specs: %#v", specs)
	}
}

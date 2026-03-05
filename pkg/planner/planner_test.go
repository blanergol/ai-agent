package planner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/blanergol/agent-core/pkg/llm"
)

// fakeLLM эмулирует провайдера LLM с заранее заданной последовательностью ответов.
type fakeLLM struct {
	// responses содержит заранее подготовленные JSON-ответы по порядку вызовов.
	responses []json.RawMessage
	// calls считает, сколько раз планировщик обращался к LLM.
	calls int
}

// Name возвращает фиктивное имя провайдера для тестов.
func (f *fakeLLM) Name() string { return "fake" }

// Chat в этих тестах не используется, но нужен для реализации интерфейса.
func (f *fakeLLM) Chat(_ context.Context, _ []llm.Message, _ llm.ChatOptions) (string, error) {
	return "", nil
}

// ChatStream в этих тестах не используется, но нужен для реализации интерфейса.
func (f *fakeLLM) ChatStream(_ context.Context, _ []llm.Message, _ llm.ChatOptions) (<-chan llm.StreamChunk, <-chan error) {
	chunks := make(chan llm.StreamChunk)
	errs := make(chan error)
	close(chunks)
	close(errs)
	return chunks, errs
}

// ChatJSON выдаёт следующий заготовленный ответ и эмулирует последовательные попытки.
func (f *fakeLLM) ChatJSON(_ context.Context, _ []llm.Message, _ string, _ llm.ChatOptions) (json.RawMessage, error) {
	f.calls++
	if len(f.responses) == 0 {
		return nil, nil
	}
	// out - ответ текущей попытки, после чего он удаляется из очереди.
	out := f.responses[0]
	f.responses = f.responses[1:]
	return out, nil
}

// TestDefaultPlannerRetriesOnSemanticValidation проверяет ретрай после семантически неверного JSON.
func TestDefaultPlannerRetriesOnSemanticValidation(t *testing.T) {
	// provider сначала возвращает `final` без final_response, затем корректный ответ.
	provider := &fakeLLM{
		responses: []json.RawMessage{
			json.RawMessage(`{"done":true,"action":{"type":"final","reasoning_summary":"x","expected_outcome":"x"}}`),
			json.RawMessage(`{"done":true,"action":{"type":"final","reasoning_summary":"ok","expected_outcome":"ok","final_response":"done"}}`),
		},
	}

	// pl настроен на две дополнительные попытки исправления.
	pl := NewDefaultPlanner(provider, Config{MaxJSONRetries: 2})
	next, err := pl.Plan(context.Background(), Observation{UserInput: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := provider.calls, 2; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
	if next.Action.Type != "final" || next.Action.FinalResponse != "done" {
		t.Fatalf("unexpected action: %+v", next.Action)
	}
}

// TestDefaultPlannerAppliesToolSelectionPolicy проверяет retry при выборе инструмента вне каталога.
func TestDefaultPlannerAppliesToolSelectionPolicy(t *testing.T) {
	provider := &fakeLLM{
		responses: []json.RawMessage{
			json.RawMessage(`{"done":false,"action":{"type":"tool","tool_name":"missing.tool","tool_args":{},"reasoning_summary":"x","expected_outcome":"x"}}`),
			json.RawMessage(`{"done":true,"action":{"type":"final","reasoning_summary":"ok","expected_outcome":"ok","final_response":"done"}}`),
		},
	}

	pl := NewDefaultPlanner(provider, Config{MaxJSONRetries: 2})
	next, err := pl.Plan(context.Background(), Observation{
		UserInput: "test",
		ToolCatalog: []ToolSpec{
			{Name: "time.now"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := provider.calls, 2; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
	if next.Action.Type != "final" || next.Action.FinalResponse != "done" {
		t.Fatalf("unexpected action: %+v", next.Action)
	}
}

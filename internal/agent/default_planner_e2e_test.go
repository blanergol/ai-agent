package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/blanergol/agent-core/internal/guardrails"
	"github.com/blanergol/agent-core/internal/llm"
	"github.com/blanergol/agent-core/internal/memory"
	"github.com/blanergol/agent-core/internal/planner"
	"github.com/blanergol/agent-core/internal/state"
	"github.com/blanergol/agent-core/internal/tools"
)

// scriptedLLMProvider отдаёт заранее заданные JSON-ответы, имитируя детерминированный LLM.
type scriptedLLMProvider struct {
	responses []json.RawMessage
	calls     int
}

// Name возвращает техническое имя тестового провайдера.
func (p *scriptedLLMProvider) Name() string { return "scripted-llm" }

// Chat в этом сценарии не используется и возвращает пустой результат.
func (p *scriptedLLMProvider) Chat(_ context.Context, _ []llm.Message, _ llm.ChatOptions) (string, error) {
	return "", nil
}

// ChatStream возвращает закрытые каналы, так как потоковый режим в тесте не задействован.
func (p *scriptedLLMProvider) ChatStream(_ context.Context, _ []llm.Message, _ llm.ChatOptions) (<-chan llm.StreamChunk, <-chan error) {
	chunks := make(chan llm.StreamChunk)
	errs := make(chan error)
	close(chunks)
	close(errs)
	return chunks, errs
}

// ChatJSON выдаёт очередной подготовленный JSON-ответ для шага планировщика.
func (p *scriptedLLMProvider) ChatJSON(_ context.Context, _ []llm.Message, _ string, _ llm.ChatOptions) (json.RawMessage, error) {
	if p.calls >= len(p.responses) {
		return nil, nil
	}
	out := p.responses[p.calls]
	p.calls++
	return out, nil
}

// TestAgentE2EWithDefaultPlannerAndMockedLLM проверяет e2e-контракт Agent+DefaultPlanner с mock LLM.
func TestAgentE2EWithDefaultPlannerAndMockedLLM(t *testing.T) {
	provider := &scriptedLLMProvider{
		responses: []json.RawMessage{
			json.RawMessage(`{"done":false,"action":{"type":"tool","tool_name":"kv.put","tool_args":{"key":"answer","value":"42"},"reasoning_summary":"store value","expected_outcome":"state updated"}}`),
			json.RawMessage(`{"done":true,"action":{"type":"final","reasoning_summary":"completed","expected_outcome":"return response","final_response":"stored"}}`),
		},
	}
	plannerRuntime := planner.NewDefaultPlanner(provider, planner.Config{MaxJSONRetries: 1})

	mem := memory.NewManager(memory.NewShortTermMemory(20), memory.NewInMemoryLongTerm(), 5)
	store, err := state.NewKVStore("")
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	registry := tools.NewRegistry(tools.RegistryConfig{
		DefaultTimeout:      time.Second,
		MaxOutputBytes:      64 * 1024,
		MaxExecutionRetries: 0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := registry.Register(tools.NewKVPutTool(store)); err != nil {
		t.Fatalf("register kv.put: %v", err)
	}
	if err := registry.Register(tools.NewKVGetTool(store)); err != nil {
		t.Fatalf("register kv.get: %v", err)
	}

	gr := guardrails.New(guardrails.Config{
		MaxSteps:           8,
		MaxToolCalls:       8,
		MaxDuration:        time.Minute,
		MaxToolOutputBytes: 64 * 1024,
	})

	ag := NewWithConfig(
		plannerRuntime,
		mem,
		store,
		registry,
		gr,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		RuntimeConfig{
			MaxStepTimeout:      time.Second,
			ContinueOnToolError: false,
			MaxPlanningRetries:  1,
		},
		nil,
	)

	result, err := ag.RunWithInput(context.Background(), RunInput{
		Text:      "save value then answer",
		SessionID: "sess-e2e",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.FinalResponse != "stored" {
		t.Fatalf("final response = %q, want stored", result.FinalResponse)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("tool calls = %d, want 1", result.ToolCalls)
	}
	if provider.calls != 2 {
		t.Fatalf("llm calls = %d, want 2", provider.calls)
	}
	got, ok := store.GetString("session:sess-e2e:answer")
	if !ok || got != "42" {
		t.Fatalf("state value = %q, ok=%t", got, ok)
	}
}

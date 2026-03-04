package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/blanergol/agent-core/internal/apperrors"
	"github.com/blanergol/agent-core/internal/guardrails"
	"github.com/blanergol/agent-core/internal/llm"
	"github.com/blanergol/agent-core/internal/memory"
	"github.com/blanergol/agent-core/internal/planner"
	"github.com/blanergol/agent-core/internal/state"
	"github.com/blanergol/agent-core/internal/tools"
)

// flakyPlanner эмулирует временные ошибки планировщика до заданного числа неудачных вызовов.
type flakyPlanner struct {
	failures int
	calls    int
}

// Plan возвращает ошибку на первых вызовах, а затем финальное действие.
func (p *flakyPlanner) Plan(_ context.Context, _ planner.Observation) (planner.NextAction, error) {
	p.calls++
	if p.calls <= p.failures {
		return planner.NextAction{}, errors.New("temporary planner failure")
	}
	return planner.NextAction{
		Done: true,
		Action: planner.Action{
			Type:             "final",
			ReasoningSummary: "enough context",
			ExpectedOutcome:  "respond to user",
			FinalResponse:    "done",
		},
	}, nil
}

// sequencePlanner последовательно возвращает заранее подготовленные действия.
type sequencePlanner struct {
	actions []planner.NextAction
	calls   int
}

// Plan выдаёт очередное действие из сценария и фиксирует число вызовов.
func (p *sequencePlanner) Plan(_ context.Context, _ planner.Observation) (planner.NextAction, error) {
	if len(p.actions) == 0 {
		return planner.NextAction{}, errors.New("no actions configured")
	}
	idx := p.calls
	if idx >= len(p.actions) {
		idx = len(p.actions) - 1
	}
	p.calls++
	return p.actions[idx], nil
}

// recordingObserver накапливает события агента для последующих ассертов в тестах.
type recordingObserver struct {
	events []Event
}

// OnEvent сохраняет полученное событие в журнал наблюдателя.
func (o *recordingObserver) OnEvent(_ context.Context, event Event) {
	o.events = append(o.events, event)
}

// panicObserver используется для проверки recover-ветки при panic внутри observer.
type panicObserver struct{}

// OnEvent намеренно паникует, чтобы проверить защиту в notify.
func (panicObserver) OnEvent(_ context.Context, _ Event) {
	panic("observer panic")
}

// newTestAgent собирает агент с in-memory зависимостями и базовыми guardrails для unit-тестов.
func newTestAgent(t *testing.T, pl planner.Planner, cfg RuntimeConfig) *Agent {
	t.Helper()

	mem := memory.NewManager(memory.NewShortTermMemory(20), memory.NewInMemoryLongTerm(), 5)
	st, err := state.NewKVStore("")
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	reg := tools.NewRegistry(tools.RegistryConfig{
		DefaultTimeout:      time.Second,
		MaxOutputBytes:      64 * 1024,
		MaxExecutionRetries: 0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	gr := guardrails.New(guardrails.Config{
		MaxSteps:           8,
		MaxToolCalls:       8,
		MaxDuration:        time.Minute,
		MaxToolOutputBytes: 64 * 1024,
	})

	return NewWithConfig(
		pl,
		mem,
		st,
		reg,
		gr,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg,
		nil,
	)
}

// TestRunRetriesPlannerFailures проверяет retry планировщика до успешного результата.
func TestRunRetriesPlannerFailures(t *testing.T) {
	pl := &flakyPlanner{failures: 2}
	observer := &recordingObserver{}
	ag := newTestAgent(t, pl, RuntimeConfig{
		MaxStepTimeout:     time.Second,
		MaxPlanningRetries: 2,
		Observer:           observer,
	})

	result, err := ag.Run(context.Background(), "hello", guardrails.UserAuthContext{})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.FinalResponse != "done" {
		t.Fatalf("final response = %q", result.FinalResponse)
	}
	if pl.calls != 3 {
		t.Fatalf("planner calls = %d, want 3", pl.calls)
	}
	if len(observer.events) == 0 {
		t.Fatalf("observer should receive events")
	}
}

// TestRunContinuesAfterToolErrorWhenEnabled проверяет продолжение цикла при tool-ошибке в permissive-режиме.
func TestRunContinuesAfterToolErrorWhenEnabled(t *testing.T) {
	pl := &sequencePlanner{
		actions: []planner.NextAction{
			{
				Done: false,
				Action: planner.Action{
					Type:             "tool",
					ToolName:         "missing.tool",
					ToolArgs:         json.RawMessage(`{}`),
					ReasoningSummary: "try tool",
					ExpectedOutcome:  "get data",
				},
			},
			{
				Done: true,
				Action: planner.Action{
					Type:             "final",
					ReasoningSummary: "fallback after tool failure",
					ExpectedOutcome:  "return safe answer",
					FinalResponse:    "fallback answer",
				},
			},
		},
	}
	observer := &recordingObserver{}
	ag := newTestAgent(t, pl, RuntimeConfig{
		MaxStepTimeout:      time.Second,
		ContinueOnToolError: true,
		Observer:            observer,
	})

	result, err := ag.Run(context.Background(), "do something", guardrails.UserAuthContext{})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.FinalResponse != "fallback answer" {
		t.Fatalf("final response = %q", result.FinalResponse)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("tool calls = %d, want 1", result.ToolCalls)
	}
	if pl.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", pl.calls)
	}

	hasToolFailedEvent := false
	for _, event := range observer.events {
		if event.Type == EventToolFailed {
			hasToolFailedEvent = true
			break
		}
	}
	if !hasToolFailedEvent {
		t.Fatalf("expected tool_failed event")
	}
}

// TestRunFailsOnToolErrorWhenStrictMode проверяет завершение с ошибкой при strict-обработке tool-сбоев.
func TestRunFailsOnToolErrorWhenStrictMode(t *testing.T) {
	pl := &sequencePlanner{
		actions: []planner.NextAction{
			{
				Done: false,
				Action: planner.Action{
					Type:             "tool",
					ToolName:         "missing.tool",
					ToolArgs:         json.RawMessage(`{}`),
					ReasoningSummary: "try tool",
					ExpectedOutcome:  "get data",
				},
			},
		},
	}
	ag := newTestAgent(t, pl, RuntimeConfig{
		MaxStepTimeout:      time.Second,
		ContinueOnToolError: false,
	})

	if _, err := ag.Run(context.Background(), "do something", guardrails.UserAuthContext{}); err == nil {
		t.Fatalf("expected tool execution error")
	}
}

// TestRunAppliesPerToolFallbackPolicy проверяет per-tool override политики обработки ошибок инструментов.
func TestRunAppliesPerToolFallbackPolicy(t *testing.T) {
	pl := &sequencePlanner{
		actions: []planner.NextAction{
			{
				Done: false,
				Action: planner.Action{
					Type:             "tool",
					ToolName:         "missing.tool",
					ToolArgs:         json.RawMessage(`{}`),
					ReasoningSummary: "try tool",
					ExpectedOutcome:  "get data",
				},
			},
			{
				Done: true,
				Action: planner.Action{
					Type:             "final",
					ReasoningSummary: "fallback after tool failure",
					ExpectedOutcome:  "return safe answer",
					FinalResponse:    "fallback answer",
				},
			},
		},
	}
	ag := newTestAgent(t, pl, RuntimeConfig{
		MaxStepTimeout: time.Second,
		ToolErrorPolicy: NewStaticToolErrorPolicy(
			ToolErrorModeFail,
			map[string]ToolErrorMode{"missing.tool": ToolErrorModeContinue},
		),
	})

	result, err := ag.Run(context.Background(), "do something", guardrails.UserAuthContext{})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.FinalResponse != "fallback answer" {
		t.Fatalf("final response = %q", result.FinalResponse)
	}
}

// TestRunReturnsFallbackResponseOnRepeatedAction проверяет, что при зацикливании агент возвращает непустой final_response.
func TestRunReturnsFallbackResponseOnRepeatedAction(t *testing.T) {
	pl := &sequencePlanner{
		actions: []planner.NextAction{
			{
				Done: false,
				Action: planner.Action{
					Type:             "noop",
					ReasoningSummary: "wait",
					ExpectedOutcome:  "continue",
				},
			},
		},
	}
	ag := newTestAgent(t, pl, RuntimeConfig{
		MaxStepTimeout: time.Second,
	})

	result, err := ag.Run(context.Background(), "loop", guardrails.UserAuthContext{})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.StopReason != "repeated_action_detected" {
		t.Fatalf("stop reason = %q", result.StopReason)
	}
	if strings.TrimSpace(result.FinalResponse) == "" {
		t.Fatalf("final response is empty")
	}
	if !strings.Contains(result.FinalResponse, "repeated_action_detected") {
		t.Fatalf("final response = %q", result.FinalResponse)
	}
}

// TestRunReturnsFallbackResponseOnGuardrailStop проверяет fallback final_response при остановке guardrails по max steps.
func TestRunReturnsFallbackResponseOnGuardrailStop(t *testing.T) {
	pl := &sequencePlanner{
		actions: []planner.NextAction{
			{
				Done: false,
				Action: planner.Action{
					Type:             "noop",
					ReasoningSummary: "wait",
					ExpectedOutcome:  "continue",
				},
			},
		},
	}
	mem := memory.NewManager(memory.NewShortTermMemory(20), memory.NewInMemoryLongTerm(), 5)
	st, err := state.NewKVStore("")
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	reg := tools.NewRegistry(tools.RegistryConfig{
		DefaultTimeout:      time.Second,
		MaxOutputBytes:      64 * 1024,
		MaxExecutionRetries: 0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	gr := guardrails.New(guardrails.Config{
		MaxSteps:           1,
		MaxToolCalls:       8,
		MaxDuration:        time.Minute,
		MaxToolOutputBytes: 64 * 1024,
	})
	ag := NewWithConfig(
		pl,
		mem,
		st,
		reg,
		gr,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		RuntimeConfig{
			MaxStepTimeout: time.Second,
		},
		nil,
	)

	result, err := ag.Run(context.Background(), "max steps", guardrails.UserAuthContext{})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(result.StopReason, "max steps exceeded") {
		t.Fatalf("stop reason = %q", result.StopReason)
	}
	if strings.TrimSpace(result.FinalResponse) == "" {
		t.Fatalf("final response is empty")
	}
	if !strings.Contains(result.FinalResponse, "max steps exceeded") {
		t.Fatalf("final response = %q", result.FinalResponse)
	}
}

// TestRunRecoversObserverPanic проверяет, что panic observer не прерывает выполнение агента.
func TestRunRecoversObserverPanic(t *testing.T) {
	pl := &flakyPlanner{}
	ag := newTestAgent(t, pl, RuntimeConfig{
		MaxStepTimeout: time.Second,
		Observer:       panicObserver{},
	})

	result, err := ag.Run(context.Background(), "hello", guardrails.UserAuthContext{})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.FinalResponse != "done" {
		t.Fatalf("final response = %q", result.FinalResponse)
	}
}

// TestObserverReceivesRedactedUserSub проверяет редактирование пользовательского subject перед отправкой observer.
func TestObserverReceivesRedactedUserSub(t *testing.T) {
	pl := &flakyPlanner{}
	observer := &recordingObserver{}
	ag := newTestAgent(t, pl, RuntimeConfig{
		MaxStepTimeout: time.Second,
		Observer:       observer,
	})

	_, err := ag.RunWithInput(context.Background(), RunInput{
		Text: "hello",
		Auth: guardrails.UserAuthContext{
			Subject: "user-123@example.com",
		},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(observer.events) == 0 {
		t.Fatalf("expected observer events")
	}
	for _, event := range observer.events {
		if event.UserSub == "" {
			continue
		}
		if strings.Contains(event.UserSub, "user-123@example.com") {
			t.Fatalf("raw user subject leaked to observer: %s", event.UserSub)
		}
		if !strings.HasPrefix(event.UserSub, "sha256:") {
			t.Fatalf("user subject must be hashed, got %s", event.UserSub)
		}
	}
}

// TestRunRejectsTooLargeInput проверяет ограничение максимальной длины входного запроса.
func TestRunRejectsTooLargeInput(t *testing.T) {
	pl := &flakyPlanner{}
	ag := newTestAgent(t, pl, RuntimeConfig{
		MaxStepTimeout: time.Second,
		MaxInputChars:  5,
	})

	_, err := ag.Run(context.Background(), "this input is too long", guardrails.UserAuthContext{})
	if err == nil {
		t.Fatalf("expected input size error")
	}
	if apperrors.CodeOf(err) != apperrors.CodeValidation {
		t.Fatalf("error code = %s", apperrors.CodeOf(err))
	}
}

// TestRunDeterministicIDs проверяет генерацию стабильных session/correlation идентификаторов.
func TestRunDeterministicIDs(t *testing.T) {
	pl := &flakyPlanner{}
	ag := newTestAgent(t, pl, RuntimeConfig{
		MaxStepTimeout: time.Second,
		Deterministic:  true,
	})

	first, err := ag.RunWithInput(context.Background(), RunInput{
		Text: "first",
		Auth: guardrails.UserAuthContext{},
	})
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	second, err := ag.RunWithInput(context.Background(), RunInput{
		Text: "second",
		Auth: guardrails.UserAuthContext{},
	})
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if first.SessionID != "session-000001" || first.CorrelationID != "corr-000001" {
		t.Fatalf("first ids = (%s,%s)", first.SessionID, first.CorrelationID)
	}
	if second.SessionID != "session-000002" || second.CorrelationID != "corr-000002" {
		t.Fatalf("second ids = (%s,%s)", second.SessionID, second.CorrelationID)
	}
	if first.APIVersion != APIVersion || second.APIVersion != APIVersion {
		t.Fatalf("api version mismatch: first=%s second=%s", first.APIVersion, second.APIVersion)
	}
}

// memoryProbePlanner валидирует наличие восстановленного контекста из snapshot в наблюдении планировщика.
type memoryProbePlanner struct{}

// Plan завершает выполнение и проверяет, что в наблюдение попал восстановленный marker из snapshot.
func (p memoryProbePlanner) Plan(_ context.Context, obs planner.Observation) (planner.NextAction, error) {
	joined := strings.Join(obs.MemorySnippets, "\n")
	if !strings.Contains(joined, "restored marker") {
		return planner.NextAction{}, errors.New("snapshot marker is missing in memory snippets")
	}
	return planner.NextAction{
		Done: true,
		Action: planner.Action{
			Type:             "final",
			ReasoningSummary: "snapshot restored",
			ExpectedOutcome:  "respond",
			FinalResponse:    "ok",
		},
	}, nil
}

// TestRunRestoresRuntimeSnapshot проверяет restore short-term памяти и guardrails из SnapshotStore.
func TestRunRestoresRuntimeSnapshot(t *testing.T) {
	mem := memory.NewManager(memory.NewShortTermMemory(20), memory.NewInMemoryLongTerm(), 5)
	st, err := state.NewKVStore("")
	if err != nil {
		t.Fatalf("new kv store: %v", err)
	}
	reg := tools.NewRegistry(tools.RegistryConfig{
		DefaultTimeout:      time.Second,
		MaxOutputBytes:      64 * 1024,
		MaxExecutionRetries: 0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	gr := guardrails.New(guardrails.Config{
		MaxSteps:           8,
		MaxToolCalls:       8,
		MaxDuration:        time.Minute,
		MaxToolOutputBytes: 64 * 1024,
	})
	snapshots := NewInMemorySnapshotStore()
	if err := snapshots.Save(context.Background(), RuntimeSnapshot{
		APIVersion: APIVersion,
		SessionID:  "sess-restore",
		ShortTermMessages: []llm.Message{
			{Role: llm.RoleUser, Content: "restored marker"},
		},
		Guardrails: guardrails.RuntimeSnapshot{},
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	ag := NewWithConfig(
		memoryProbePlanner{},
		mem,
		st,
		reg,
		gr,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		RuntimeConfig{
			MaxStepTimeout: time.Second,
			SnapshotStore:  snapshots,
		},
		nil,
	)

	result, err := ag.RunWithInput(context.Background(), RunInput{
		Text:      "run with restore",
		SessionID: "sess-restore",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.FinalResponse != "ok" {
		t.Fatalf("final response = %q", result.FinalResponse)
	}
}

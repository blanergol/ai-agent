package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/blanergol/agent-core/pkg/llm"
	"github.com/blanergol/agent-core/pkg/telemetry"
)

// recordingLongTerm — тестовый double для LongTermMemory с накоплением вызовов.
type recordingLongTerm struct {
	items         []Item
	recallItems   []Item
	storeCalls    int
	recallCalls   int
	deleteCalls   int
	lastRecallTop int
}

// Store фиксирует запись элемента и увеличивает счётчик вызовов.
func (m *recordingLongTerm) Store(_ context.Context, item Item) error {
	m.storeCalls++
	m.items = append(m.items, item)
	return nil
}

// Recall возвращает заранее заданный набор элементов и сохраняет topK последнего вызова.
func (m *recordingLongTerm) Recall(_ context.Context, _ string, topK int) ([]Item, error) {
	m.recallCalls++
	m.lastRecallTop = topK
	out := make([]Item, len(m.recallItems))
	copy(out, m.recallItems)
	return out, nil
}

// Get ищет элемент по ID в локально сохранённых тестовых данных.
func (m *recordingLongTerm) Get(_ context.Context, id string) (Item, bool, error) {
	for _, item := range m.items {
		if item.ID == id {
			return item, true, nil
		}
	}
	return Item{}, false, nil
}

// Delete увеличивает счётчик удалений без изменения локального набора.
func (m *recordingLongTerm) Delete(_ context.Context, _ string) error {
	m.deleteCalls++
	return nil
}

// tracerProbe — тестовый tracer, который запоминает имена стартованных span.
type tracerProbe struct {
	started []string
}

// Start сохраняет имя span и возвращает no-op реализацию telemetry.Span.
func (t *tracerProbe) Start(ctx context.Context, name string, _ map[string]any) (context.Context, telemetry.Span) {
	t.started = append(t.started, name)
	return ctx, noopSpanProbe{}
}

// noopSpanProbe реализует telemetry.Span без побочных эффектов для тестов.
type noopSpanProbe struct{}

// AddEvent игнорирует события в no-op span.
func (noopSpanProbe) AddEvent(_ string, _ map[string]any) {}

// End завершает no-op span без действий.
func (noopSpanProbe) End(_ error) {}

// TestAddUserMessageRedactsSensitiveData проверяет privacy-policy записи в long-term память.
func TestAddUserMessageRedactsSensitiveData(t *testing.T) {
	longTerm := &recordingLongTerm{}
	manager := NewManagerWithOptions(NewShortTermMemory(10), longTerm, 5, 2048)

	if err := manager.AddUserMessage(context.Background(), "api_key=SECRET123 plain"); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	if longTerm.storeCalls != 1 {
		t.Fatalf("store calls = %d, want 1", longTerm.storeCalls)
	}
	got := longTerm.items[0].Text
	if strings.Contains(got, "SECRET123") {
		t.Fatalf("secret leaked into long-term memory: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("redaction marker not found in stored text: %s", got)
	}
}

// TestBuildContextTrimsByBudgetWithoutRecall проверяет ограничение token budget даже без recalled items.
func TestBuildContextTrimsByBudgetWithoutRecall(t *testing.T) {
	longTerm := &recordingLongTerm{}
	manager := NewManagerWithOptions(NewShortTermMemory(10), longTerm, 5, 10)
	manager.shortTerm.Add(llm.Message{Role: llm.RoleUser, Content: strings.Repeat("a", 60)})
	manager.shortTerm.Add(llm.Message{Role: llm.RoleAssistant, Content: strings.Repeat("b", 60)})

	ctxMessages, err := manager.BuildContext(context.Background(), "query")
	if err != nil {
		t.Fatalf("build context: %v", err)
	}
	if len(ctxMessages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(ctxMessages))
	}
	if ctxMessages[0].Content != "Context was truncated to fit token budget." {
		t.Fatalf("unexpected summary message: %s", ctxMessages[0].Content)
	}
}

// TestManagerEmitsMemorySpans проверяет наличие tracing spans для ключевых memory-операций.
func TestManagerEmitsMemorySpans(t *testing.T) {
	longTerm := &recordingLongTerm{}
	longTerm.recallItems = []Item{{ID: "i1", Text: "memory snippet"}}
	manager := NewManagerWithOptions(NewShortTermMemory(10), longTerm, 3, 2048)
	tracer := &tracerProbe{}
	ctx := telemetry.WithTracer(context.Background(), tracer)

	if err := manager.AddAssistantMessage(ctx, "hello"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}
	if _, err := manager.BuildContext(ctx, "hello"); err != nil {
		t.Fatalf("build context: %v", err)
	}

	assertStartedSpan(t, tracer.started, "memory.add_assistant_message")
	assertStartedSpan(t, tracer.started, "memory.build_context")
	assertStartedSpan(t, tracer.started, "memory.recall")
}

// assertStartedSpan проверяет, что tracer зафиксировал ожидаемый span.
func assertStartedSpan(t *testing.T, started []string, name string) {
	t.Helper()
	for _, got := range started {
		if got == name {
			return
		}
	}
	t.Fatalf("span %q was not started; started=%v", name, started)
}

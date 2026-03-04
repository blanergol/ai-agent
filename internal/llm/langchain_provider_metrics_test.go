package llm

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/blanergol/agent-core/internal/telemetry"
	"github.com/tmc/langchaingo/llms"
)

// staticChatExecutor — тестовый executor с фиксированным ответом и ошибкой.
type staticChatExecutor struct {
	response *llms.ContentResponse
	err      error
}

// GenerateContent возвращает заранее настроенный результат без обращения к внешнему провайдеру.
func (e *staticChatExecutor) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	return e.response, e.err
}

// metricProbe собирает метрики для последующих проверок в тестах.
type metricProbe struct {
	counters []counterRecord
	hists    []histRecord
}

// counterRecord хранит один вызов счётчика.
type counterRecord struct {
	name  string
	delta int64
	tags  map[string]string
}

// histRecord хранит одну запись гистограммы.
type histRecord struct {
	name  string
	value float64
	tags  map[string]string
}

// IncCounter записывает событие счётчика в локальный буфер.
func (m *metricProbe) IncCounter(name string, delta int64, tags map[string]string) {
	m.counters = append(m.counters, counterRecord{name: name, delta: delta, tags: copyTags(tags)})
}

// ObserveHistogram записывает событие гистограммы в локальный буфер.
func (m *metricProbe) ObserveHistogram(name string, value float64, tags map[string]string) {
	m.hists = append(m.hists, histRecord{name: name, value: value, tags: copyTags(tags)})
}

// copyTags копирует map тегов, чтобы тестовые записи не зависели от внешних мутаций.
func copyTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	out := make(map[string]string, len(tags))
	for key, value := range tags {
		out[key] = value
	}
	return out
}

// hasCounter проверяет наличие счётчика по имени и опциональному тегу kind.
func (m *metricProbe) hasCounter(name string, kind string) bool {
	for _, rec := range m.counters {
		if rec.name != name {
			continue
		}
		if kind == "" || rec.tags["kind"] == kind {
			return true
		}
	}
	return false
}

// counterDelta суммирует значения счётчика по имени и опциональному kind.
func (m *metricProbe) counterDelta(name, kind string) int64 {
	var total int64
	for _, rec := range m.counters {
		if rec.name != name {
			continue
		}
		if kind != "" && rec.tags["kind"] != kind {
			continue
		}
		total += rec.delta
	}
	return total
}

// hasHistogram проверяет, что гистограмма с указанным именем была записана.
func (m *metricProbe) hasHistogram(name string) bool {
	for _, rec := range m.hists {
		if rec.name == name {
			return true
		}
	}
	return false
}

// TestLangChainProviderEmitsTokenUsageMetrics проверяет публикацию token-usage метрик после успешного Chat.
func TestLangChainProviderEmitsTokenUsageMetrics(t *testing.T) {
	exec := &staticChatExecutor{
		response: &llms.ContentResponse{
			Choices: []*llms.ContentChoice{{
				Content: "hello world",
				GenerationInfo: map[string]any{
					"PromptTokens":     11,
					"CompletionTokens": 7,
					"TotalTokens":      18,
				},
			}},
		},
	}
	provider := newLangChainProvider(
		"test",
		"test-model",
		exec,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Second,
		0,
		time.Millisecond,
		1,
		5,
		time.Second,
		true,
		0,
		nil,
		nil,
	)

	metrics := &metricProbe{}
	ctx := telemetry.WithMetrics(context.Background(), metrics)
	_, err := provider.Chat(ctx, []Message{{Role: RoleUser, Content: "say hello"}}, ChatOptions{})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	if !metrics.hasCounter("llm.tokens", "prompt") {
		t.Fatalf("missing llm.tokens counter for prompt")
	}
	if !metrics.hasCounter("llm.tokens", "completion") {
		t.Fatalf("missing llm.tokens counter for completion")
	}
	if !metrics.hasCounter("llm.tokens", "total") {
		t.Fatalf("missing llm.tokens counter for total")
	}
	if !metrics.hasHistogram("llm.tokens.prompt") {
		t.Fatalf("missing llm.tokens.prompt histogram")
	}
	if !metrics.hasHistogram("llm.tokens.completion") {
		t.Fatalf("missing llm.tokens.completion histogram")
	}
	if !metrics.hasHistogram("llm.tokens.total") {
		t.Fatalf("missing llm.tokens.total histogram")
	}
	if got := metrics.counterDelta("llm.tokens", "prompt"); got != 11 {
		t.Fatalf("prompt token delta = %d, want 11", got)
	}
	if got := metrics.counterDelta("llm.tokens", "completion"); got != 7 {
		t.Fatalf("completion token delta = %d, want 7", got)
	}
	if got := metrics.counterDelta("llm.tokens", "total"); got != 18 {
		t.Fatalf("total token delta = %d, want 18", got)
	}
}

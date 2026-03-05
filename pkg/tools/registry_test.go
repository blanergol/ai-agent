package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/blanergol/agent-core/pkg/cache"
	"github.com/blanergol/agent-core/pkg/telemetry"
)

// testTool — универсальная заглушка инструмента для проверок реестра.
type testTool struct {
	// name позволяет переиспользовать заглушку для разных имён инструментов.
	name string
	// outputSchema позволяет проверять валидацию разных форматов output.
	outputSchema string
	// output содержит результат, который вернёт Execute.
	output string
}

// Name возвращает имя тестового инструмента.
func (t *testTool) Name() string { return t.name }

// Description возвращает фиксированное описание для теста.
func (t *testTool) Description() string { return "test" }

// InputSchema принимает любой объект.
func (t *testTool) InputSchema() string { return `{"type":"object"}` }

// OutputSchema принимает любой тип результата.
func (t *testTool) OutputSchema() string {
	if t.outputSchema != "" {
		return t.outputSchema
	}
	return `{"type":"string"}`
}

// Execute возвращает стабильный успешный результат.
func (t *testTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	if t.output != "" {
		return ToolResult{Output: t.output}, nil
	}
	return ToolResult{Output: "ok"}, nil
}

// TestRegistryBlocksToolOutsideAllowlist проверяет блокировку инструмента, отсутствующего в allowlist.
func TestRegistryBlocksToolOutsideAllowlist(t *testing.T) {
	// reg настроен так, что разрешён только time.now.
	reg := NewRegistry(RegistryConfig{
		Allowlist:      []string{"time.now"},
		DefaultTimeout: time.Second,
	}, slog.Default())

	if err := reg.Register(&testTool{name: "http.get"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	// err должен сообщать, что инструмент не входит в allowlist.
	_, err := reg.Execute(context.Background(), "http.get", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected allowlist error")
	}
}

// TestRegistrySpecsAreSortedByName проверяет лексикографическую сортировку списка Specs.
func TestRegistrySpecsAreSortedByName(t *testing.T) {
	reg := NewRegistry(RegistryConfig{DefaultTimeout: time.Second}, slog.Default())
	if err := reg.Register(&testTool{name: "zeta"}); err != nil {
		t.Fatalf("register zeta: %v", err)
	}
	if err := reg.Register(&testTool{name: "alpha"}); err != nil {
		t.Fatalf("register alpha: %v", err)
	}

	specs := reg.Specs()
	if len(specs) != 2 {
		t.Fatalf("specs len = %d", len(specs))
	}
	if specs[0].Name != "alpha" || specs[1].Name != "zeta" {
		t.Fatalf("spec order = %s,%s", specs[0].Name, specs[1].Name)
	}
}

// TestRegistrySpecsExcludeDisallowedTools проверяет, что planner не получает инструменты вне allowlist.
func TestRegistrySpecsExcludeDisallowedTools(t *testing.T) {
	reg := NewRegistry(RegistryConfig{
		Allowlist:      []string{"time.now"},
		DefaultTimeout: time.Second,
	}, slog.Default())
	if err := reg.Register(&testTool{name: "time.now"}); err != nil {
		t.Fatalf("register time.now: %v", err)
	}
	if err := reg.Register(&testTool{name: "kv.get"}); err != nil {
		t.Fatalf("register kv.get: %v", err)
	}

	specs := reg.Specs()
	if len(specs) != 1 {
		t.Fatalf("specs len = %d, want 1", len(specs))
	}
	if specs[0].Name != "time.now" {
		t.Fatalf("spec name = %s, want time.now", specs[0].Name)
	}
}

// TestRegistryValidatesToolOutputSchema проверяет отказ при несоответствии output JSON-схеме.
func TestRegistryValidatesToolOutputSchema(t *testing.T) {
	reg := NewRegistry(RegistryConfig{DefaultTimeout: time.Second}, slog.Default())
	if err := reg.Register(&testTool{
		name:         "bad.output",
		outputSchema: `{"type":"integer"}`,
		output:       "not-json-int",
	}); err != nil {
		t.Fatalf("register bad.output: %v", err)
	}

	_, err := reg.Execute(context.Background(), "bad.output", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected output schema validation error")
	}
	if apperrors.CodeOf(err) != apperrors.CodeValidation {
		t.Fatalf("error code = %s", apperrors.CodeOf(err))
	}
}

// countingTool считает число фактических выполнений инструмента.
type countingTool struct {
	name  string
	calls int
}

// Name возвращает имя инструмента-счётчика.
func (t *countingTool) Name() string { return t.name }

// Description возвращает описание инструмента-счётчика.
func (t *countingTool) Description() string { return "counting" }

// InputSchema задаёт схему аргументов инструмента-счётчика.
func (t *countingTool) InputSchema() string { return `{"type":"object"}` }

// OutputSchema задаёт строковый формат результата инструмента-счётчика.
func (t *countingTool) OutputSchema() string {
	return `{"type":"string"}`
}

// Execute увеличивает счётчик вызовов и возвращает номер успешного выполнения.
func (t *countingTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	t.calls++
	return ToolResult{Output: fmt.Sprintf("ok-%d", t.calls)}, nil
}

// flakyRetryableTool имитирует временные сбои, после которых инструмент восстанавливается.
type flakyRetryableTool struct {
	name      string
	failUntil int
	calls     int
}

// Name возвращает имя retryable-заглушки.
func (t *flakyRetryableTool) Name() string { return t.name }

// Description возвращает описание retryable-заглушки.
func (t *flakyRetryableTool) Description() string { return "flaky retryable" }

// InputSchema задаёт схему аргументов retryable-заглушки.
func (t *flakyRetryableTool) InputSchema() string { return `{"type":"object"}` }

// OutputSchema задаёт строковый формат результата retryable-заглушки.
func (t *flakyRetryableTool) OutputSchema() string {
	return `{"type":"string"}`
}

// IsReadOnly помечает retryable-заглушку как read-only.
func (t *flakyRetryableTool) IsReadOnly() bool { return true }

// IsSafeRetry разрешает безопасные повторные попытки для retryable-заглушки.
func (t *flakyRetryableTool) IsSafeRetry() bool { return true }

// Execute возвращает временную ошибку до заданного числа попыток.
func (t *flakyRetryableTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	t.calls++
	if t.calls <= t.failUntil {
		return ToolResult{}, apperrors.New(apperrors.CodeTransient, "temporary failure", true)
	}
	return ToolResult{Output: "ok"}, nil
}

// flakyNonRetryableTool имитирует постоянный сбой инструмента.
type flakyNonRetryableTool struct {
	name  string
	calls int
}

// Name возвращает имя non-retryable-заглушки.
func (t *flakyNonRetryableTool) Name() string { return t.name }

// Description возвращает описание non-retryable-заглушки.
func (t *flakyNonRetryableTool) Description() string { return "flaky non-retryable" }

// InputSchema задаёт схему аргументов non-retryable-заглушки.
func (t *flakyNonRetryableTool) InputSchema() string { return `{"type":"object"}` }

// OutputSchema задаёт строковый формат результата non-retryable-заглушки.
func (t *flakyNonRetryableTool) OutputSchema() string {
	return `{"type":"string"}`
}

// IsReadOnly помечает non-retryable-заглушку как read-only.
func (t *flakyNonRetryableTool) IsReadOnly() bool { return true }

// IsSafeRetry объявляет повтор допустимым по интерфейсу, но ошибки остаются постоянными.
func (t *flakyNonRetryableTool) IsSafeRetry() bool { return true }

// Execute всегда возвращает постоянную ошибку после инкремента счётчика.
func (t *flakyNonRetryableTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	t.calls++
	return ToolResult{}, errors.New("permanent failure")
}

// cachedReadOnlyTool имитирует read-only инструмент с кэшируемым результатом.
type cachedReadOnlyTool struct {
	name  string
	calls int
	ttl   time.Duration
}

// Name возвращает имя кэшируемой заглушки.
func (t *cachedReadOnlyTool) Name() string { return t.name }

// Description возвращает описание кэшируемой заглушки.
func (t *cachedReadOnlyTool) Description() string { return "cached read-only" }

// InputSchema задаёт схему аргументов кэшируемой заглушки.
func (t *cachedReadOnlyTool) InputSchema() string { return `{"type":"object"}` }

// OutputSchema задаёт строковый формат результата кэшируемой заглушки.
func (t *cachedReadOnlyTool) OutputSchema() string {
	return `{"type":"string"}`
}

// IsReadOnly помечает кэшируемую заглушку как read-only.
func (t *cachedReadOnlyTool) IsReadOnly() bool { return true }

// IsSafeRetry указывает, что повтор для кэшируемой заглушки безопасен.
func (t *cachedReadOnlyTool) IsSafeRetry() bool { return true }

// CacheTTL возвращает TTL кэша результата для кэшируемой заглушки.
func (t *cachedReadOnlyTool) CacheTTL() time.Duration { return t.ttl }

// Execute возвращает результат с номером фактического выполнения.
func (t *cachedReadOnlyTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	t.calls++
	return ToolResult{Output: fmt.Sprintf("cached-%d", t.calls)}, nil
}

// TestRegistryDedupIsSessionScoped проверяет, что dedup mutating-инструмента не пересекает session_id.
func TestRegistryDedupIsSessionScoped(t *testing.T) {
	reg := NewRegistry(RegistryConfig{
		DefaultTimeout: time.Second,
		DedupTTL:       time.Minute,
	}, slog.Default())
	tool := &countingTool{name: "counting.mutating"}
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}

	args := json.RawMessage(`{"k":"v"}`)
	ctxA := telemetry.WithSession(context.Background(), telemetry.SessionInfo{
		SessionID:     "session-a",
		CorrelationID: "corr-a",
	})
	ctxB := telemetry.WithSession(context.Background(), telemetry.SessionInfo{
		SessionID:     "session-b",
		CorrelationID: "corr-b",
	})

	firstA, err := reg.Execute(ctxA, tool.Name(), args)
	if err != nil {
		t.Fatalf("execute firstA: %v", err)
	}
	if firstA.Output != "ok-1" {
		t.Fatalf("firstA output = %s", firstA.Output)
	}

	secondA, err := reg.Execute(ctxA, tool.Name(), args)
	if err != nil {
		t.Fatalf("execute secondA: %v", err)
	}
	if secondA.Output != "ok-1" {
		t.Fatalf("secondA output = %s, want deduped ok-1", secondA.Output)
	}

	firstB, err := reg.Execute(ctxB, tool.Name(), args)
	if err != nil {
		t.Fatalf("execute firstB: %v", err)
	}
	if firstB.Output != "ok-2" {
		t.Fatalf("firstB output = %s, want fresh execution ok-2", firstB.Output)
	}
	if tool.calls != 2 {
		t.Fatalf("calls = %d, want 2", tool.calls)
	}
}

// TestRegistryRetriesRetryableErrorsWithBackoff проверяет retry/backoff для retryable-ошибок инструмента.
func TestRegistryRetriesRetryableErrorsWithBackoff(t *testing.T) {
	reg := NewRegistry(RegistryConfig{
		DefaultTimeout:      time.Second,
		MaxExecutionRetries: 3,
		RetryBase:           20 * time.Millisecond,
	}, slog.Default())
	tool := &flakyRetryableTool{name: "flaky.retryable", failUntil: 2}
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}

	start := time.Now()
	result, err := reg.Execute(context.Background(), tool.Name(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	elapsed := time.Since(start)
	if result.Output != "ok" {
		t.Fatalf("result output = %s", result.Output)
	}
	if tool.calls != 3 {
		t.Fatalf("calls = %d, want 3", tool.calls)
	}
	if elapsed < 45*time.Millisecond {
		t.Fatalf("elapsed = %s, want retry backoff delay", elapsed)
	}
}

// TestRegistryDoesNotRetryNonRetryableErrors проверяет, что non-retryable ошибки не повторяются.
func TestRegistryDoesNotRetryNonRetryableErrors(t *testing.T) {
	reg := NewRegistry(RegistryConfig{
		DefaultTimeout:      time.Second,
		MaxExecutionRetries: 3,
		RetryBase:           20 * time.Millisecond,
	}, slog.Default())
	tool := &flakyNonRetryableTool{name: "flaky.nonretryable"}
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := reg.Execute(context.Background(), tool.Name(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("expected execute error")
	}
	if tool.calls != 1 {
		t.Fatalf("calls = %d, want 1", tool.calls)
	}
}

// TestRegistryExposesToolMutability проверяет доступность mutability-метаданных для approval слоя.
func TestRegistryExposesToolMutability(t *testing.T) {
	reg := NewRegistry(RegistryConfig{DefaultTimeout: time.Second}, slog.Default())
	ro := &cachedReadOnlyTool{name: "read.tool", ttl: time.Second}
	mut := &testTool{name: "mut.tool"}
	if err := reg.Register(ro); err != nil {
		t.Fatalf("register read-only tool: %v", err)
	}
	if err := reg.Register(mut); err != nil {
		t.Fatalf("register mutating tool: %v", err)
	}

	readOnly, known := reg.IsReadOnlyTool("read.tool")
	if !known || !readOnly {
		t.Fatalf("read.tool readOnly=%t known=%t, want true true", readOnly, known)
	}

	readOnly, known = reg.IsReadOnlyTool("mut.tool")
	if !known || readOnly {
		t.Fatalf("mut.tool readOnly=%t known=%t, want false true", readOnly, known)
	}
}

// TestRegistryCachesReadOnlyTools проверяет общий read-only кэш для повторных одинаковых вызовов.
func TestRegistryCachesReadOnlyTools(t *testing.T) {
	reg := NewRegistry(RegistryConfig{
		DefaultTimeout: time.Second,
	}, slog.Default())
	tool := &cachedReadOnlyTool{name: "cached.readonly", ttl: time.Minute}
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := telemetry.WithSession(context.Background(), telemetry.SessionInfo{
		SessionID:     "session-cache",
		CorrelationID: "corr-cache",
	})
	args := json.RawMessage(`{"q":"hello"}`)

	first, err := reg.Execute(ctx, tool.Name(), args)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	second, err := reg.Execute(ctx, tool.Name(), args)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if first.Output != "cached-1" || second.Output != "cached-1" {
		t.Fatalf("unexpected outputs: first=%q second=%q", first.Output, second.Output)
	}
	if tool.calls != 1 {
		t.Fatalf("calls = %d, want 1 due read-cache", tool.calls)
	}
}

// TestRegistrySharesReadCacheAcrossInstancesViaBackplane проверяет shared read-cache между инстансами.
func TestRegistrySharesReadCacheAcrossInstancesViaBackplane(t *testing.T) {
	backplane := cache.NewFileBackplane(t.TempDir())
	regA := NewRegistry(RegistryConfig{
		DefaultTimeout: time.Second,
		CacheBackplane: backplane,
	}, slog.Default())
	regB := NewRegistry(RegistryConfig{
		DefaultTimeout: time.Second,
		CacheBackplane: backplane,
	}, slog.Default())

	toolA := &cachedReadOnlyTool{name: "cached.shared", ttl: time.Minute}
	toolB := &cachedReadOnlyTool{name: "cached.shared", ttl: time.Minute}
	if err := regA.Register(toolA); err != nil {
		t.Fatalf("register toolA: %v", err)
	}
	if err := regB.Register(toolB); err != nil {
		t.Fatalf("register toolB: %v", err)
	}

	ctx := telemetry.WithSession(context.Background(), telemetry.SessionInfo{
		SessionID:     "session-shared",
		CorrelationID: "corr-shared",
	})
	args := json.RawMessage(`{"q":"hello"}`)

	if _, err := regA.Execute(ctx, toolA.Name(), args); err != nil {
		t.Fatalf("regA execute: %v", err)
	}
	if _, err := regB.Execute(ctx, toolB.Name(), args); err != nil {
		t.Fatalf("regB execute: %v", err)
	}
	if toolA.calls != 1 {
		t.Fatalf("toolA calls = %d, want 1", toolA.calls)
	}
	if toolB.calls != 0 {
		t.Fatalf("toolB calls = %d, want 0 due shared backplane cache", toolB.calls)
	}
}

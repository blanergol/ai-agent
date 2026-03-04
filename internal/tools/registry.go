package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/blanergol/agent-core/internal/apperrors"
	"github.com/blanergol/agent-core/internal/cache"
	"github.com/blanergol/agent-core/internal/redact"
	"github.com/blanergol/agent-core/internal/telemetry"
	"github.com/xeipuuv/gojsonschema"
)

// RegistryConfig задаёт политики доступа, тайм-ауты и кэширование для реестра инструментов.
type RegistryConfig struct {
	// Allowlist задаёт явный набор разрешённых инструментов.
	Allowlist []string
	// Denylist задаёт принудительно запрещённые инструменты.
	Denylist []string
	// DefaultTimeout применяется к вызову инструмента, если нет точечного значения.
	DefaultTimeout time.Duration
	// MaxOutputBytes ограничивает размер текстового результата инструмента.
	MaxOutputBytes int
	// ToolTimeouts позволяет задавать тайм-аут индивидуально для каждого инструмента.
	ToolTimeouts map[string]time.Duration
	// MaxExecutionRetries задаёт число повторных попыток выполнения инструмента.
	MaxExecutionRetries int
	// RetryBase задаёт базовую задержку экспоненциального backoff между retry-попытками.
	RetryBase time.Duration
	// MaxParallel ограничивает число одновременных вызовов инструментов в процессе.
	MaxParallel int
	// DedupTTL задаёт окно дедупликации для повторных вызовов mutating-инструментов.
	DedupTTL time.Duration
	// CacheBackplane задаёт общий cache backend для multi-instance runtime.
	CacheBackplane cache.Backplane
}

// Registry хранит инструменты и управляет их безопасным выполнением.
type Registry struct {
	mu sync.RWMutex

	// tools хранит зарегистрированные инструменты по имени.
	tools map[string]Tool
	// cfg определяет политики доступа и ограничения выполнения.
	cfg RegistryConfig
	// log пишет аудиторные события вызовов инструментов.
	log *slog.Logger
	// limiter ограничивает число одновременно исполняемых инструментов.
	limiter chan struct{}
	// dedupMu защищает кэш результатов mutating-инструментов.
	dedupMu sync.Mutex
	// dedup хранит недавние результаты для idempotency ключей.
	dedup map[string]dedupEntry
	// readCacheMu защищает кэш read-only инструментов.
	readCacheMu sync.Mutex
	// readCache хранит краткоживущие результаты безопасных read-only вызовов.
	readCache map[string]readCacheEntry
	// cacheBackplane расширяет локальный cache общим process-external storage.
	cacheBackplane cache.Backplane
}

// dedupEntry хранит запись дедупликации для mutating-инструмента.
type dedupEntry struct {
	// result хранит результат предыдущего успешного вызова.
	result ToolResult
	// expiresAt ограничивает срок валидности записи дедупликации.
	expiresAt time.Time
}

// readCacheEntry хранит кэшированный результат read-only инструмента.
type readCacheEntry struct {
	// result хранит результат read-only вызова.
	result ToolResult
	// expiresAt ограничивает срок валидности записи.
	expiresAt time.Time
}

// Пространства имён для хранения дедупликации и read-cache в общем backplane.
const (
	toolDedupNamespace = "tools.dedup"
	toolReadNamespace  = "tools.read_cache"
)

// NewRegistry создаёт реестр инструментов и нормализует дефолты конфигурации.
func NewRegistry(cfg RegistryConfig, logger *slog.Logger) *Registry {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 5 * time.Second
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 64 * 1024
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 16
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = 200 * time.Millisecond
	}
	if cfg.DedupTTL <= 0 {
		cfg.DedupTTL = 10 * time.Minute
	}
	return &Registry{
		tools: make(map[string]Tool),
		cfg:   cfg,
		log:   logger,
		limiter: func() chan struct{} {
			if cfg.MaxParallel <= 0 {
				return nil
			}
			return make(chan struct{}, cfg.MaxParallel)
		}(),
		dedup:          make(map[string]dedupEntry),
		readCache:      make(map[string]readCacheEntry),
		cacheBackplane: cfg.CacheBackplane,
	}
}

// Register валидирует и добавляет инструмент в реестр без дубликатов.
func (r *Registry) Register(tool Tool) error {
	if tool == nil {
		return errors.New("tool is nil")
	}
	name := strings.TrimSpace(tool.Name())
	if name == "" {
		return errors.New("tool name is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("tool already registered: %s", name)
	}
	r.tools[name] = tool
	return nil
}

// MustRegister добавляет инструмент или аварийно завершает запуск при ошибке.
func (r *Registry) MustRegister(tool Tool) {
	if err := r.Register(tool); err != nil {
		panic(err)
	}
}

// Specs возвращает каталог зарегистрированных инструментов для планировщика.
func (r *Registry) Specs() []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// out содержит сериализуемые спецификации всех инструментов.
	out := make([]Spec, 0, len(r.tools))
	for _, tool := range r.tools {
		name := strings.TrimSpace(tool.Name())
		// В planner каталог попадают только инструменты, реально доступные по policy.
		if err := r.checkPolicy(name); err != nil {
			continue
		}
		out = append(out, Spec{
			Name:         name,
			Description:  tool.Description(),
			InputSchema:  tool.InputSchema(),
			OutputSchema: tool.OutputSchema(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Execute валидирует политику и аргументы, затем запускает инструмент с тайм-аутом и ретраями.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	// start нужен для метрики длительности в audit-логе.
	start := time.Now()
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "tool.execute", map[string]any{"tool": name})
	var spanErr error
	defer func() { span.End(spanErr) }()
	if args == nil {
		args = json.RawMessage("{}")
	}
	// argsHash и idempotencyKey упрощают корреляцию повторных вызовов в логах.
	// Dedup scope привязан к session_id, чтобы избежать кросс-сессионных ложных попаданий.
	argsHash := hashBytes(args)
	idempotencyKey := name + ":" + telemetry.SessionScopeFromContext(ctx) + ":" + argsHash

	// tool - зарегистрированная реализация по имени.
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		err := apperrors.Wrap(apperrors.CodeNotFound, fmt.Sprintf("tool not found: %s", name), nil, false)
		spanErr = err
		return ToolResult{}, err
	}
	if err := r.checkPolicy(name); err != nil {
		r.audit(ctx, name, idempotencyKey, argsHash, time.Since(start), false, err)
		spanErr = err
		return ToolResult{}, err
	}
	if err := validateArgs(tool.InputSchema(), args); err != nil {
		wrapped := apperrors.Wrap(apperrors.CodeValidation, "invalid tool args", err, false)
		r.audit(ctx, name, idempotencyKey, argsHash, time.Since(start), false, wrapped)
		spanErr = wrapped
		return ToolResult{}, wrapped
	}
	telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
		Kind:    telemetry.ArtifactPrompt,
		Name:    "tool.args." + name,
		Payload: string(args),
	})
	readOnly, safeRetry := toolRetryFlags(tool)
	if !readOnly {
		if cached, ok := r.loadDedup(ctx, idempotencyKey); ok {
			r.audit(ctx, name, idempotencyKey, argsHash, time.Since(start), true, nil)
			telemetry.MetricsFromContext(ctx).IncCounter("tool.calls", 1, map[string]string{"tool": name, "status": "dedup"})
			return cached, nil
		}
	} else if cacheTTL := toolCacheTTL(tool); cacheTTL > 0 {
		if cached, ok := r.loadReadCache(ctx, idempotencyKey); ok {
			r.audit(ctx, name, idempotencyKey, argsHash, time.Since(start), true, nil)
			telemetry.MetricsFromContext(ctx).IncCounter("tool.calls", 1, map[string]string{"tool": name, "status": "cache"})
			return cached, nil
		}
	}
	release, err := r.acquire(ctx)
	if err != nil {
		wrapped := apperrors.Wrap(apperrors.CodeRateLimit, "tool execution is limited by concurrency policy", err, true)
		r.audit(ctx, name, idempotencyKey, argsHash, time.Since(start), false, wrapped)
		spanErr = wrapped
		return ToolResult{}, wrapped
	}
	defer release()

	// timeout выбирается по умолчанию либо из индивидуальной настройки инструмента.
	timeout := r.cfg.DefaultTimeout
	if v, ok := r.cfg.ToolTimeouts[name]; ok && v > 0 {
		timeout = v
	}
	maxRetries := r.cfg.MaxExecutionRetries
	if !readOnly && !safeRetry {
		// Для mutating-инструментов без явного safe-retry отключаем автоматические ретраи.
		maxRetries = 0
	}
	// lastErr хранит последнюю ошибку, если все попытки завершились неуспешно.
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// tctx ограничивает продолжительность одной попытки выполнения.
		tctx, cancel := context.WithTimeout(ctx, timeout)
		res, err := tool.Execute(tctx, args)
		cancel()
		if err != nil {
			lastErr = err
			telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
				Kind:    telemetry.ArtifactState,
				Name:    "tool.error." + name,
				Payload: redact.Error(err),
			})
			retryable := isRetryableToolError(err)
			if attempt >= maxRetries || !retryable {
				break
			}
			telemetry.MetricsFromContext(ctx).IncCounter("tool.retries", 1, map[string]string{"tool": name})
			span.AddEvent("tool.retry", map[string]any{
				"tool":    name,
				"attempt": attempt + 1,
			})
			if err := sleepToolBackoff(ctx, r.cfg.RetryBase, attempt); err != nil {
				lastErr = err
				break
			}
			continue
		}
		// Санитизируем недоверенный output инструмента перед сохранением/передачей в планировщик.
		res.Output = sanitizeToolOutput(res.Output)
		if len(res.Output) > r.cfg.MaxOutputBytes {
			err = apperrors.New(apperrors.CodeValidation, fmt.Sprintf("tool output exceeded max size: %d", len(res.Output)), false)
			r.audit(ctx, name, idempotencyKey, argsHash, time.Since(start), false, err)
			spanErr = err
			return ToolResult{}, err
		}
		if err := validateOutput(tool.OutputSchema(), res.Output); err != nil {
			wrapped := apperrors.Wrap(apperrors.CodeValidation, "invalid tool output", err, false)
			r.audit(ctx, name, idempotencyKey, argsHash, time.Since(start), false, wrapped)
			spanErr = wrapped
			return ToolResult{}, wrapped
		}
		if !readOnly {
			r.storeDedup(ctx, idempotencyKey, res)
		} else if cacheTTL := toolCacheTTL(tool); cacheTTL > 0 {
			r.storeReadCache(ctx, idempotencyKey, res, cacheTTL)
		}
		telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
			Kind:    telemetry.ArtifactResponse,
			Name:    "tool.result." + name,
			Payload: res.Output,
		})
		r.audit(ctx, name, idempotencyKey, argsHash, time.Since(start), true, nil)
		telemetry.MetricsFromContext(ctx).IncCounter("tool.calls", 1, map[string]string{"tool": name, "status": "ok"})
		telemetry.MetricsFromContext(ctx).ObserveHistogram("tool.latency_ms", float64(time.Since(start).Milliseconds()), map[string]string{"tool": name})
		return res, nil
	}

	wrapped := apperrors.Wrap(
		apperrors.CodeTransient,
		"tool execution failed after retries",
		lastErr,
		isRetryableToolError(lastErr),
	)
	r.audit(ctx, name, idempotencyKey, argsHash, time.Since(start), false, wrapped)
	telemetry.MetricsFromContext(ctx).IncCounter("tool.calls", 1, map[string]string{"tool": name, "status": "error"})
	telemetry.MetricsFromContext(ctx).ObserveHistogram("tool.latency_ms", float64(time.Since(start).Milliseconds()), map[string]string{"tool": name})
	spanErr = wrapped
	return ToolResult{}, wrapped
}

// checkPolicy применяет denylist/allowlist политику к имени инструмента.
func (r *Registry) checkPolicy(name string) error {
	if contains(r.cfg.Denylist, name) {
		return apperrors.New(apperrors.CodeForbidden, fmt.Sprintf("tool denied by policy: %s", name), false)
	}
	if len(r.cfg.Allowlist) > 0 && !contains(r.cfg.Allowlist, name) {
		return apperrors.New(apperrors.CodeForbidden, fmt.Sprintf("tool not in allowlist: %s", name), false)
	}
	return nil
}

// audit пишет структурированный лог о результате выполнения инструмента.
func (r *Registry) audit(ctx context.Context, tool string, idempotencyKey string, argsHash string, duration time.Duration, ok bool, err error) {
	// attrs содержит базовый набор полей для трассировки вызова.
	attrs := []slog.Attr{
		slog.String("tool", tool),
		slog.String("idempotency_key", idempotencyKey),
		slog.String("args_hash", argsHash),
		slog.Duration("duration", duration),
		slog.Bool("success", ok),
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", redact.Error(err)))
	}
	telemetry.NewContextLogger(ctx, r.log).Info("tool_execution", attrs...)
}

// acquire резервирует слот конкурентности для вызова инструмента.
func (r *Registry) acquire(ctx context.Context) (func(), error) {
	if r.limiter == nil {
		return func() {}, nil
	}
	select {
	case r.limiter <- struct{}{}:
		return func() { <-r.limiter }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// contains выполняет регистронезависимую проверку наличия инструмента в списке.
func contains(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}

// hashBytes возвращает короткий SHA-256 хэш аргументов вызова.
func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// validateArgs проверяет JSON-аргументы по JSON Schema инструмента.
func validateArgs(schema string, args json.RawMessage) error {
	if strings.TrimSpace(schema) == "" {
		return nil
	}
	// compiled - скомпилированная схема для валидации args.
	compiled, err := gojsonschema.NewSchema(gojsonschema.NewStringLoader(schema))
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}
	// res содержит детальный результат валидации.
	res, err := compiled.Validate(gojsonschema.NewBytesLoader(args))
	if err != nil {
		return fmt.Errorf("validate args: %w", err)
	}
	if res.Valid() {
		return nil
	}
	return errors.New(res.Errors()[0].String())
}

// validateOutput проверяет строковый результат инструмента по его выходной JSON-схеме.
func validateOutput(schema string, output string) error {
	if strings.TrimSpace(schema) == "" {
		return nil
	}
	compiled, err := gojsonschema.NewSchema(gojsonschema.NewStringLoader(schema))
	if err != nil {
		return fmt.Errorf("compile output schema: %w", err)
	}
	var decoded any
	var res *gojsonschema.Result
	if decodeErr := json.Unmarshal([]byte(output), &decoded); decodeErr == nil {
		res, err = compiled.Validate(gojsonschema.NewGoLoader(decoded))
	} else {
		res, err = compiled.Validate(gojsonschema.NewGoLoader(output))
	}
	if err != nil {
		return fmt.Errorf("validate output: %w", err)
	}
	if res.Valid() {
		return nil
	}
	return errors.New(res.Errors()[0].String())
}

// toolRetryFlags извлекает retry-политику инструмента или возвращает консервативные дефолты.
func toolRetryFlags(tool Tool) (readOnly bool, safeRetry bool) {
	policy, ok := tool.(RetryPolicy)
	if !ok {
		return false, false
	}
	return policy.IsReadOnly(), policy.IsSafeRetry()
}

// toolCacheTTL извлекает TTL read-cache политики инструмента.
func toolCacheTTL(tool Tool) time.Duration {
	policy, ok := tool.(CachePolicy)
	if !ok {
		return 0
	}
	ttl := policy.CacheTTL()
	if ttl <= 0 {
		return 0
	}
	return ttl
}

// isRetryableToolError определяет, нужно ли выполнять повторную попытку после ошибки инструмента.
func isRetryableToolError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if apperrors.RetryableOf(err) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// sleepToolBackoff выполняет экспоненциальную паузу между retry-попытками или прерывается по ctx.
func sleepToolBackoff(ctx context.Context, base time.Duration, attempt int) error {
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 8 {
		attempt = 8
	}
	delay := base * time.Duration(1<<attempt)
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loadDedup ищет кэшированный результат mutating-инструмента по idempotency ключу.
func (r *Registry) loadDedup(ctx context.Context, key string) (ToolResult, bool) {
	r.dedupMu.Lock()
	entry, ok := r.dedup[key]
	if ok && time.Now().After(entry.expiresAt) {
		delete(r.dedup, key)
		ok = false
	}
	r.dedupMu.Unlock()
	if ok {
		return entry.result, true
	}
	if r.cacheBackplane == nil {
		return ToolResult{}, false
	}
	cachedEntry, ok, err := r.cacheBackplane.Load(ctx, toolDedupNamespace, key)
	if err != nil {
		r.logBackplaneError(ctx, "tool dedup backplane load failed", err)
		return ToolResult{}, false
	}
	if !ok {
		return ToolResult{}, false
	}
	result, err := decodeCachedToolResult(cachedEntry.Value)
	if err != nil {
		return ToolResult{}, false
	}
	r.dedupMu.Lock()
	r.dedup[key] = dedupEntry{result: result, expiresAt: cachedEntry.ExpiresAt}
	r.dedupMu.Unlock()
	return result, true
}

// storeDedup сохраняет результат mutating-инструмента для безопасного повторного запроса.
func (r *Registry) storeDedup(ctx context.Context, key string, result ToolResult) {
	expiresAt := time.Now().Add(r.cfg.DedupTTL)
	r.dedupMu.Lock()
	r.dedup[key] = dedupEntry{result: result, expiresAt: expiresAt}
	r.dedupMu.Unlock()
	if r.cacheBackplane == nil {
		return
	}
	payload, err := encodeCachedToolResult(result)
	if err != nil {
		return
	}
	if err := r.cacheBackplane.Store(ctx, toolDedupNamespace, key, cache.Entry{
		Value:     payload,
		ExpiresAt: expiresAt,
	}); err != nil {
		r.logBackplaneError(ctx, "tool dedup backplane store failed", err)
	}
}

// loadReadCache ищет кэшированный результат read-only инструмента.
func (r *Registry) loadReadCache(ctx context.Context, key string) (ToolResult, bool) {
	r.readCacheMu.Lock()
	entry, ok := r.readCache[key]
	if ok && time.Now().After(entry.expiresAt) {
		delete(r.readCache, key)
		ok = false
	}
	r.readCacheMu.Unlock()
	if ok {
		return entry.result, true
	}
	if r.cacheBackplane == nil {
		return ToolResult{}, false
	}
	cachedEntry, ok, err := r.cacheBackplane.Load(ctx, toolReadNamespace, key)
	if err != nil {
		r.logBackplaneError(ctx, "tool read-cache backplane load failed", err)
		return ToolResult{}, false
	}
	if !ok {
		return ToolResult{}, false
	}
	result, err := decodeCachedToolResult(cachedEntry.Value)
	if err != nil {
		return ToolResult{}, false
	}
	r.readCacheMu.Lock()
	r.readCache[key] = readCacheEntry{result: result, expiresAt: cachedEntry.ExpiresAt}
	r.readCacheMu.Unlock()
	return result, true
}

// storeReadCache сохраняет результат read-only инструмента в кэш.
func (r *Registry) storeReadCache(ctx context.Context, key string, result ToolResult, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	expiresAt := time.Now().Add(ttl)
	r.readCacheMu.Lock()
	r.readCache[key] = readCacheEntry{
		result:    result,
		expiresAt: expiresAt,
	}
	r.readCacheMu.Unlock()
	if r.cacheBackplane == nil {
		return
	}
	payload, err := encodeCachedToolResult(result)
	if err != nil {
		return
	}
	if err := r.cacheBackplane.Store(ctx, toolReadNamespace, key, cache.Entry{
		Value:     payload,
		ExpiresAt: expiresAt,
	}); err != nil {
		r.logBackplaneError(ctx, "tool read-cache backplane store failed", err)
	}
}

// encodeCachedToolResult сериализует результат инструмента для хранения в backplane-кэше.
func encodeCachedToolResult(result ToolResult) (string, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// decodeCachedToolResult десериализует результат инструмента из строки backplane-кэша.
func decodeCachedToolResult(raw string) (ToolResult, error) {
	var out ToolResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return ToolResult{}, err
	}
	return out, nil
}

// logBackplaneError пишет предупреждение об ошибке внешнего cache backplane.
func (r *Registry) logBackplaneError(ctx context.Context, msg string, err error) {
	telemetry.NewContextLogger(ctx, r.log).Warn(msg, slog.String("error", redact.Error(err)))
}

// sanitizeToolOutput очищает неожиданные байты и невалидный UTF-8 из результата инструмента.
func sanitizeToolOutput(input string) string {
	if input == "" {
		return input
	}
	clean := strings.ReplaceAll(input, "\x00", "")
	if utf8.ValidString(clean) {
		return clean
	}
	// fallback удаляет невалидные rune, сохраняя максимальный объём полезного текста.
	out := make([]rune, 0, len(clean))
	for _, r := range clean {
		if r == utf8.RuneError {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

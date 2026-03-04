package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blanergol/agent-core/internal/apperrors"
	"github.com/blanergol/agent-core/internal/cache"
	"github.com/blanergol/agent-core/internal/redact"
	"github.com/blanergol/agent-core/internal/telemetry"
	"github.com/tmc/langchaingo/llms"
	"github.com/xeipuuv/gojsonschema"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// jsonBlockRegexp извлекает JSON, если модель вернула его внутри markdown fence.
var jsonBlockRegexp = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*\\}|\\[.*\\])\\s*```")

// chatExecutor абстрагирует минимальный интерфейс вызова chat-модели.
type chatExecutor interface {
	// GenerateContent отправляет сообщения в модель и возвращает набор кандидатов ответа.
	GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error)
}

// langChainProvider реализует Provider поверх chatExecutor с retry, cache и circuit breaker.
type langChainProvider struct {
	// name идентифицирует активный backend провайдера в логах.
	name string
	// modelName stores concrete model identifier for generation analytics/cost attribution.
	modelName string
	// model абстрагирует конкретный клиент langchaingo.
	model chatExecutor
	// logger пишет диагностические события запросов и ретраев.
	logger *slog.Logger
	// timeout ограничивает длительность одного обращения к модели.
	timeout time.Duration
	// maxRetries задаёт число повторных попыток при ошибках.
	maxRetries int
	// retryBase задаёт базовую задержку для backoff между попытками.
	retryBase time.Duration
	// limiter ограничивает число одновременных запросов к LLM.
	limiter chan struct{}
	// breaker защищает внешнюю зависимость от каскадных сбоев.
	breaker *circuitBreaker
	// disableJitter выключает случайную компоненту backoff в детерминированном режиме.
	disableJitter bool
	// cacheTTL задаёт TTL кэша chat-ответов (0 отключает кэш).
	cacheTTL time.Duration
	// cacheMu защищает кэш ответов от гонок.
	cacheMu sync.Mutex
	// cache хранит ответы chat по ключу запроса.
	cache map[string]cacheEntry
	// cacheBackplane расширяет локальный cache общим process-external storage.
	cacheBackplane cache.Backplane
	// modelPrices stores optional per-model fallback prices for Langfuse cost details.
	modelPrices map[string]ModelPrice
}

// cacheEntry хранит кэшированный текст ответа и связанную с ним служебную информацию.
type cacheEntry struct {
	// text хранит кэшированный финальный текст ответа модели.
	text string
	// expiresAt задаёт TTL-ограничение записи кэша.
	expiresAt time.Time
	// usage хранит token usage, сохранённый вместе с ответом.
	usage tokenUsage
}

// tokenUsage описывает расход токенов по одному вызову модели.
type tokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Source           string
	InputCost        float64
	OutputCost       float64
	TotalCost        float64
	CostSource       string
}

// hasCost reports whether cost fields contain meaningful data.
func (u tokenUsage) hasCost() bool {
	if strings.TrimSpace(u.CostSource) != "" {
		return true
	}
	return u.InputCost != 0 || u.OutputCost != 0 || u.TotalCost != 0
}

// newLangChainProvider нормализует дефолты и создаёт провайдер поверх chatExecutor.
func newLangChainProvider(
	name string,
	modelName string,
	model chatExecutor,
	logger *slog.Logger,
	timeout time.Duration,
	maxRetries int,
	retryBase time.Duration,
	maxParallel int,
	breakerFailures int,
	breakerCooldown time.Duration,
	disableJitter bool,
	cacheTTL time.Duration,
	cacheBackplane cache.Backplane,
	modelPrices map[string]ModelPrice,
) *langChainProvider {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	if retryBase <= 0 {
		retryBase = 300 * time.Millisecond
	}
	if maxParallel <= 0 {
		maxParallel = 4
	}
	return &langChainProvider{
		name:           name,
		modelName:      strings.TrimSpace(modelName),
		model:          model,
		logger:         logger,
		timeout:        timeout,
		maxRetries:     maxRetries,
		retryBase:      retryBase,
		limiter:        make(chan struct{}, maxParallel),
		breaker:        newCircuitBreaker(breakerFailures, breakerCooldown),
		disableJitter:  disableJitter,
		cacheTTL:       cacheTTL,
		cache:          make(map[string]cacheEntry),
		cacheBackplane: cacheBackplane,
		modelPrices:    cloneModelPrices(modelPrices),
	}
}

func cloneModelPrices(modelPrices map[string]ModelPrice) map[string]ModelPrice {
	if len(modelPrices) == 0 {
		return nil
	}
	cloned := make(map[string]ModelPrice, len(modelPrices))
	for model, price := range modelPrices {
		key := strings.ToLower(strings.TrimSpace(model))
		if key == "" {
			continue
		}
		if price.InputPer1M < 0 || price.OutputPer1M < 0 {
			continue
		}
		cloned[key] = price
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func (p *langChainProvider) enrichUsageWithCost(usage tokenUsage) tokenUsage {
	if usage.TotalTokens <= 0 && usage.PromptTokens > 0 && usage.CompletionTokens > 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if usage.hasCost() {
		if usage.TotalCost <= 0 && (usage.InputCost > 0 || usage.OutputCost > 0) {
			usage.TotalCost = usage.InputCost + usage.OutputCost
		}
		if strings.TrimSpace(usage.CostSource) == "" {
			usage.CostSource = "provider_native"
		}
		return usage
	}

	key := strings.ToLower(strings.TrimSpace(p.modelName))
	if key == "" {
		return usage
	}
	price, ok := p.modelPrices[key]
	if !ok {
		return usage
	}
	usage.InputCost = (float64(maxInt(usage.PromptTokens, 0)) / 1_000_000.0) * price.InputPer1M
	usage.OutputCost = (float64(maxInt(usage.CompletionTokens, 0)) / 1_000_000.0) * price.OutputPer1M
	usage.TotalCost = usage.InputCost + usage.OutputCost
	usage.CostSource = "model_pricing"
	return usage
}

func maxInt(v, minValue int) int {
	if v < minValue {
		return minValue
	}
	return v
}

// Name возвращает системный идентификатор провайдера.
func (p *langChainProvider) Name() string {
	return p.name
}

// Chat выполняет текстовый запрос к модели с тайм-аутом, лимитером и повторными попытками.
func (p *langChainProvider) Chat(ctx context.Context, messages []Message, opts ChatOptions) (string, error) {
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "llm.chat", map[string]any{
		"provider": p.name,
	})
	var spanErr error
	defer func() { span.End(spanErr) }()
	setLangfuseGenerationStaticAttributes(ctx, p.modelName, opts)
	startedAt := time.Now().UTC()

	if len(messages) == 0 {
		err := apperrors.New(apperrors.CodeValidation, "messages is empty", false)
		spanErr = err
		return "", err
	}
	if err := p.breaker.Allow(); err != nil {
		spanErr = err
		return "", err
	}
	release, err := p.acquire(ctx)
	if err != nil {
		wrapped := apperrors.Wrap(apperrors.CodeRateLimit, "llm call concurrency limit reached", err, true)
		spanErr = wrapped
		return "", wrapped
	}
	defer release()

	// promptHash нужен для корреляции запроса и ответа без утечки исходного текста.
	promptHash := hashMessages(messages)
	cacheKey := p.cacheKey(ctx, messages, opts)
	if cached, ok := p.loadCache(ctx, cacheKey); ok {
		telemetry.NewContextLogger(ctx, p.logger).Debug("llm chat cache hit",
			slog.String("provider", p.name),
			slog.String("prompt_hash", promptHash),
			slog.String("response_hash", hashString(cached.text)),
		)
		metrics := telemetry.MetricsFromContext(ctx)
		metrics.IncCounter("llm.calls", 1, map[string]string{"provider": p.name, "status": "cache"})
		usage := cached.usage
		if !usage.valid() {
			usage = estimatedUsage(messages, cached.text)
		}
		usage = p.enrichUsageWithCost(usage)
		setLangfuseGenerationUsageAndCostAttributes(ctx, usage)
		setLangfuseCompletionStartTime(ctx, startedAt)
		emitTokenUsageMetrics(metrics, p.name, usage)
		return cached.text, nil
	}
	telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
		Kind:    telemetry.ArtifactPrompt,
		Name:    "llm.chat.messages",
		Payload: artifactMessages(messages),
	})
	// lastErr хранит последнюю ошибку после исчерпания ретраев.
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		// callCtx ограничивает каждую попытку отдельным timeout.
		callCtx, cancel := context.WithTimeout(ctx, p.timeout)
		resp, err := p.model.GenerateContent(callCtx, toLangChainMessages(messages), buildCallOptions(opts)...)
		cancel()
		if err == nil {
			// text содержит первую текстовую альтернативу ответа модели после trim.
			text := strings.TrimSpace(firstChoiceText(resp))
			p.breaker.MarkSuccess()
			telemetry.NewContextLogger(ctx, p.logger).Debug("llm chat complete",
				slog.String("provider", p.name),
				slog.String("prompt_hash", promptHash),
				slog.String("response_hash", hashString(text)),
				slog.Int("attempt", attempt+1),
			)
			usage := usageFromResponse(messages, text, resp)
			usage = p.enrichUsageWithCost(usage)
			metrics := telemetry.MetricsFromContext(ctx)
			metrics.IncCounter("llm.calls", 1, map[string]string{"provider": p.name, "status": "ok"})
			setLangfuseGenerationUsageAndCostAttributes(ctx, usage)
			setLangfuseCompletionStartTime(ctx, startedAt)
			emitTokenUsageMetrics(metrics, p.name, usage)
			span.AddEvent("llm.token_usage", map[string]any{
				"prompt_tokens":     usage.PromptTokens,
				"completion_tokens": usage.CompletionTokens,
				"total_tokens":      usage.TotalTokens,
				"source":            usage.Source,
			})
			p.storeCache(ctx, cacheKey, text, usage)
			telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
				Kind:    telemetry.ArtifactResponse,
				Name:    "llm.chat.response",
				Payload: text,
			})
			return text, nil
		}

		classified := classifyLLMError(err)
		lastErr = classified
		p.breaker.MarkFailure()
		telemetry.NewContextLogger(ctx, p.logger).Warn("llm chat retry",
			slog.String("provider", p.name),
			slog.String("prompt_hash", promptHash),
			slog.Int("attempt", attempt+1),
			slog.String("error", sanitizeLogValue(classified.Error())),
		)
		if !apperrors.RetryableOf(classified) || attempt >= p.maxRetries {
			break
		}
		sleepWithBackoff(ctx, p.retryBase, attempt, p.disableJitter)
	}

	telemetry.MetricsFromContext(ctx).IncCounter("llm.calls", 1, map[string]string{"provider": p.name, "status": "error"})
	if lastErr == nil {
		lastErr = apperrors.New(apperrors.CodeTransient, "llm call failed", true)
	}
	spanErr = apperrors.Wrap(apperrors.CodeTransient, "chat failed after retries", lastErr, true)
	return "", spanErr
}

// ChatStream возвращает поток частичных фрагментов; для базового каркаса стрим
// использует provider-native streaming callback и поддерживает отмену через context.
func (p *langChainProvider) ChatStream(ctx context.Context, messages []Message, opts ChatOptions) (<-chan StreamChunk, <-chan error) {
	chunks := make(chan StreamChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "llm.chat_stream", map[string]any{
			"provider": p.name,
		})
		var spanErr error
		defer func() { span.End(spanErr) }()
		setLangfuseGenerationStaticAttributes(ctx, p.modelName, opts)
		startedAt := time.Now().UTC()
		firstChunkAt := time.Time{}

		if len(messages) == 0 {
			spanErr = apperrors.New(apperrors.CodeValidation, "messages is empty", false)
			errs <- spanErr
			return
		}

		cacheKey := p.cacheKey(ctx, messages, opts)
		if cached, ok := p.loadCache(ctx, cacheKey); ok {
			usage := cached.usage
			if !usage.valid() {
				usage = estimatedUsage(messages, cached.text)
			}
			usage = p.enrichUsageWithCost(usage)
			setLangfuseGenerationUsageAndCostAttributes(ctx, usage)
			setLangfuseCompletionStartTime(ctx, startedAt)
			for _, delta := range splitStreamChunks(cached.text, 48) {
				select {
				case <-ctx.Done():
					spanErr = ctx.Err()
					errs <- spanErr
					return
				case chunks <- StreamChunk{Delta: delta}:
				}
			}
			chunks <- StreamChunk{Done: true}
			return
		}

		if err := p.breaker.Allow(); err != nil {
			spanErr = err
			errs <- err
			return
		}
		release, err := p.acquire(ctx)
		if err != nil {
			spanErr = apperrors.Wrap(apperrors.CodeRateLimit, "llm call concurrency limit reached", err, true)
			errs <- spanErr
			return
		}
		defer release()

		var lastErr error
		for attempt := 0; attempt <= p.maxRetries; attempt++ {
			var builder strings.Builder
			streamed := false
			callOptions := append(buildCallOptions(opts), llms.WithStreamingFunc(func(streamCtx context.Context, chunk []byte) error {
				delta := string(chunk)
				if delta == "" {
					return nil
				}
				if firstChunkAt.IsZero() {
					firstChunkAt = time.Now().UTC()
				}
				builder.WriteString(delta)
				streamed = true
				select {
				case <-streamCtx.Done():
					return streamCtx.Err()
				case chunks <- StreamChunk{Delta: delta}:
					return nil
				}
			}))

			callCtx, cancel := context.WithTimeout(ctx, p.timeout)
			resp, callErr := p.model.GenerateContent(callCtx, toLangChainMessages(messages), callOptions...)
			cancel()

			if callErr == nil {
				text := strings.TrimSpace(builder.String())
				if text == "" {
					text = strings.TrimSpace(firstChoiceText(resp))
				}
				p.breaker.MarkSuccess()
				usage := usageFromResponse(messages, text, resp)
				usage = p.enrichUsageWithCost(usage)
				p.storeCache(ctx, cacheKey, text, usage)
				metrics := telemetry.MetricsFromContext(ctx)
				metrics.IncCounter("llm.calls", 1, map[string]string{"provider": p.name, "status": "ok"})
				setLangfuseGenerationUsageAndCostAttributes(ctx, usage)
				if firstChunkAt.IsZero() {
					setLangfuseCompletionStartTime(ctx, startedAt)
				} else {
					setLangfuseCompletionStartTime(ctx, firstChunkAt)
				}
				emitTokenUsageMetrics(metrics, p.name, usage)
				chunks <- StreamChunk{Done: true}
				return
			}

			classified := classifyLLMError(callErr)
			lastErr = classified
			p.breaker.MarkFailure()
			if streamed || !apperrors.RetryableOf(classified) || attempt >= p.maxRetries {
				break
			}
			sleepWithBackoff(ctx, p.retryBase, attempt, p.disableJitter)
		}
		if lastErr == nil {
			lastErr = apperrors.New(apperrors.CodeTransient, "llm stream failed", true)
		}
		spanErr = apperrors.Wrap(apperrors.CodeTransient, "chat stream failed after retries", lastErr, true)
		errs <- spanErr
	}()

	return chunks, errs
}

// ChatJSON принуждает модель вернуть валидный JSON и проверяет его по JSON Schema.
func (p *langChainProvider) ChatJSON(ctx context.Context, messages []Message, jsonSchema string, opts ChatOptions) (json.RawMessage, error) {
	if strings.TrimSpace(jsonSchema) == "" {
		return nil, apperrors.New(apperrors.CodeValidation, "json schema is empty", false)
	}

	// validator компилирует схему один раз, чтобы валидировать все попытки.
	validator, err := gojsonschema.NewSchema(gojsonschema.NewStringLoader(jsonSchema))
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeValidation, "compile json schema", err, false)
	}

	// promptMessages расширяется инструкциями о строгом JSON-формате.
	promptMessages := append([]Message{}, messages...)
	promptMessages = append(promptMessages, Message{
		Role: RoleSystem,
		Content: "Return ONLY JSON that matches the provided schema. " +
			"Do not include markdown fences, extra keys, or explanations.",
	})
	promptMessages = append(promptMessages, Message{
		Role:    RoleSystem,
		Content: "JSON schema: " + jsonSchema,
	})

	// lastErr аккумулирует последнюю причину неуспешной попытки.
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		text, err := p.Chat(ctx, promptMessages, opts)
		if err != nil {
			lastErr = err
			continue
		}

		// raw - потенциальный JSON-ответ модели, извлечённый из текста.
		raw, err := extractJSON(text)
		if err != nil {
			lastErr = err
			promptMessages = append(promptMessages, Message{
				Role:    RoleSystem,
				Content: "Your last response was not valid JSON. Return corrected JSON only.",
			})
			continue
		}

		// result содержит подробности валидации по JSON-схеме.
		result, err := validator.Validate(gojsonschema.NewBytesLoader(raw))
		if err != nil {
			lastErr = err
			continue
		}
		if result.Valid() {
			return raw, nil
		}

		lastErr = apperrors.New(apperrors.CodeValidation, fmt.Sprintf("json schema validation failed: %s", result.Errors()[0]), false)
		promptMessages = append(promptMessages, Message{
			Role:    RoleSystem,
			Content: "The previous JSON did not match schema. Return corrected JSON only.",
		})
	}

	return nil, apperrors.Wrap(apperrors.CodeValidation, "chat json failed after retries", lastErr, false)
}

// toLangChainMessages преобразует внутренний формат сообщений в структуру langchaingo.
func toLangChainMessages(messages []Message) []llms.MessageContent {
	// out сохраняет порядок сообщений при минимальных аллокациях.
	out := make([]llms.MessageContent, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			out = append(out, llms.TextParts(llms.ChatMessageTypeSystem, msg.Content))
		case RoleUser:
			out = append(out, llms.TextParts(llms.ChatMessageTypeHuman, msg.Content))
		case RoleTool:
			out = append(out, llms.TextParts(llms.ChatMessageTypeAI, "Tool output (untrusted): "+msg.Content))
		default:
			out = append(out, llms.TextParts(llms.ChatMessageTypeAI, msg.Content))
		}
	}
	return out
}

// buildCallOptions собирает опции генерации только из явно заданных параметров.
func buildCallOptions(opts ChatOptions) []llms.CallOption {
	// options передаются напрямую в langchaingo вызов.
	options := make([]llms.CallOption, 0, 4)
	if !math.IsNaN(opts.Temperature) {
		options = append(options, llms.WithTemperature(opts.Temperature))
	}
	if opts.MaxTokens > 0 {
		options = append(options, llms.WithMaxTokens(opts.MaxTokens))
	}
	if !math.IsNaN(opts.TopP) && opts.TopP > 0 {
		options = append(options, llms.WithTopP(opts.TopP))
	}
	if opts.Seed != 0 {
		options = append(options, llms.WithSeed(opts.Seed))
	}
	return options
}

func setLangfuseGenerationStaticAttributes(ctx context.Context, modelName string, opts ChatOptions) {
	span := oteltrace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.String("langfuse.observation.type", "generation"))
	if v := strings.TrimSpace(modelName); v != "" {
		span.SetAttributes(attribute.String("langfuse.observation.model.name", v))
	}

	params := map[string]any{}
	if !math.IsNaN(opts.Temperature) {
		params["temperature"] = opts.Temperature
	}
	if !math.IsNaN(opts.TopP) && opts.TopP > 0 {
		params["top_p"] = opts.TopP
	}
	if opts.MaxTokens > 0 {
		params["max_tokens"] = opts.MaxTokens
	}
	if opts.Seed != 0 {
		params["seed"] = opts.Seed
	}
	if len(params) == 0 {
		return
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return
	}
	span.SetAttributes(attribute.String("langfuse.observation.model.parameters", string(raw)))
}

func setLangfuseGenerationUsageAndCostAttributes(ctx context.Context, usage tokenUsage) {
	span := oteltrace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}
	if usage.valid() {
		usageDetails := map[string]any{
			"input":  usage.PromptTokens,
			"output": usage.CompletionTokens,
			"total":  usage.TotalTokens,
		}
		raw, err := json.Marshal(usageDetails)
		if err == nil {
			span.SetAttributes(attribute.String("langfuse.observation.usage_details", string(raw)))
		}
	}
	if usage.hasCost() {
		costDetails := map[string]any{
			"input":    usage.InputCost,
			"output":   usage.OutputCost,
			"total":    usage.TotalCost,
			"currency": "USD",
		}
		raw, err := json.Marshal(costDetails)
		if err == nil {
			span.SetAttributes(attribute.String("langfuse.observation.cost_details", string(raw)))
		}
	}
}

func setLangfuseCompletionStartTime(ctx context.Context, completionStart time.Time) {
	if completionStart.IsZero() {
		return
	}
	span := oteltrace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.String("langfuse.observation.completion_start_time", completionStart.UTC().Format(time.RFC3339Nano)))
}

// firstChoiceText безопасно извлекает текст первого варианта ответа.
func firstChoiceText(resp *llms.ContentResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Content
}

// sleepWithBackoff ждёт экспоненциальную паузу с jitter или прерывается по контексту.
func sleepWithBackoff(ctx context.Context, base time.Duration, attempt int, disableJitter bool) {
	if attempt < 0 {
		attempt = 0
	}
	// d - экспоненциальная составляющая задержки для текущей попытки.
	d := time.Duration(float64(base) * math.Pow(2, float64(attempt)))
	// jitter уменьшает риск синхронных повторных запросов в production-режиме.
	jitter := time.Duration(0)
	if !disableJitter {
		jitter = time.Duration(rand.Intn(100)) * time.Millisecond
	}
	// t завершает ожидание по таймеру без busy loop.
	t := time.NewTimer(d + jitter)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// hashMessages возвращает короткий хэш массива сообщений для корреляции логов.
func hashMessages(messages []Message) string {
	// b сериализует сообщения в стабильный формат перед хэшированием.
	b, _ := json.Marshal(messages)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// hashString возвращает короткий SHA-256 хэш строки.
func hashString(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:8])
}

// cacheKey строит стабильный ключ кэша для chat/chat_stream на основе prompt и опций генерации.
func (p *langChainProvider) cacheKey(_ context.Context, messages []Message, opts ChatOptions) string {
	if p.cacheTTL <= 0 {
		return ""
	}
	parts := []string{
		"v1",
		p.name,
		hashMessages(messages),
		hashChatOptions(opts),
	}
	return strings.Join(parts, ":")
}

// loadCache возвращает кэшированный ответ и очищает протухшие записи.
func (p *langChainProvider) loadCache(ctx context.Context, key string) (cacheEntry, bool) {
	if p.cacheTTL <= 0 || strings.TrimSpace(key) == "" {
		return cacheEntry{}, false
	}
	now := time.Now()
	p.cacheMu.Lock()
	entry, ok := p.cache[key]
	if ok && now.After(entry.expiresAt) {
		delete(p.cache, key)
		ok = false
	}
	p.cacheMu.Unlock()
	if ok {
		return entry, true
	}
	if p.cacheBackplane == nil {
		return cacheEntry{}, false
	}
	cachedEntry, ok, err := p.cacheBackplane.Load(ctx, p.cacheNamespace(), key)
	if err != nil || !ok {
		return cacheEntry{}, false
	}
	record, err := decodeCachedResponse(cachedEntry.Value)
	if err != nil {
		return cacheEntry{}, false
	}
	entry = cacheEntry{
		text:      record.Text,
		expiresAt: cachedEntry.ExpiresAt,
		usage: tokenUsage{
			PromptTokens:     record.PromptTokens,
			CompletionTokens: record.CompletionTokens,
			TotalTokens:      record.TotalTokens,
			Source:           record.Source,
		},
	}
	p.cacheMu.Lock()
	p.cache[key] = entry
	p.cacheMu.Unlock()
	return entry, true
}

// storeCache сохраняет успешный chat-ответ в in-memory кэш на ограниченное время.
func (p *langChainProvider) storeCache(ctx context.Context, key, text string, usage tokenUsage) {
	if p.cacheTTL <= 0 || strings.TrimSpace(key) == "" || strings.TrimSpace(text) == "" {
		return
	}
	now := time.Now()
	entry := cacheEntry{
		text:      text,
		expiresAt: now.Add(p.cacheTTL),
		usage:     usage,
	}
	p.cacheMu.Lock()
	p.cache[key] = entry

	// Периодически удаляем протухшие записи, чтобы кэш не рос бесконечно.
	if len(p.cache) > 1024 {
		for k, v := range p.cache {
			if now.After(v.expiresAt) {
				delete(p.cache, k)
			}
		}
	}
	p.cacheMu.Unlock()
	if p.cacheBackplane == nil {
		return
	}
	raw, err := encodeCachedResponse(cachedResponse{
		Text:             text,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		Source:           usage.Source,
	})
	if err != nil {
		return
	}
	_ = p.cacheBackplane.Store(ctx, p.cacheNamespace(), key, cache.Entry{
		Value:     raw,
		ExpiresAt: entry.expiresAt,
	})
}

// cacheNamespace возвращает namespace для shared backplane-кэша текущего провайдера.
func (p *langChainProvider) cacheNamespace() string {
	return "llm." + p.name
}

// cachedResponse задаёт формат сериализации ответа в external cache backplane.
type cachedResponse struct {
	Text             string `json:"text"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Source           string `json:"source"`
}

// encodeCachedResponse сериализует объект cachedResponse в строку JSON.
func encodeCachedResponse(v cachedResponse) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// decodeCachedResponse десериализует JSON-строку кэша в структуру cachedResponse.
func decodeCachedResponse(raw string) (cachedResponse, error) {
	var out cachedResponse
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return cachedResponse{}, err
	}
	return out, nil
}

// hashChatOptions формирует короткий детерминированный хеш параметров генерации.
func hashChatOptions(opts ChatOptions) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		normalizeFloatForHash(opts.Temperature),
		normalizeFloatForHash(opts.TopP),
		strconv.Itoa(opts.Seed),
		strconv.Itoa(opts.MaxTokens),
	}, "|")))
	return hex.EncodeToString(sum[:8])
}

// normalizeFloatForHash стабилизирует float-значения перед вычислением хеша.
func normalizeFloatForHash(v float64) string {
	if math.IsNaN(v) {
		return "nan"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// valid проверяет, что usage содержит хотя бы одно ненулевое значение токенов.
func (u tokenUsage) valid() bool {
	return u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0
}

// usageFromResponse извлекает usage из ответа провайдера или вычисляет оценку fallback-ом.
func usageFromResponse(messages []Message, text string, resp *llms.ContentResponse) tokenUsage {
	if usage, ok := providerNativeUsage(resp); ok {
		return usage
	}
	return estimatedUsage(messages, text)
}

// estimatedUsage оценивает usage по длине prompt и ответа.
func estimatedUsage(messages []Message, text string) tokenUsage {
	promptTokens := estimateMessageTokens(messages)
	completionTokens := estimateTextTokens(text)
	totalTokens := promptTokens + completionTokens
	return tokenUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		Source:           "estimated",
	}
}

// providerNativeUsage пытается прочитать usage из GenerationInfo провайдера.
func providerNativeUsage(resp *llms.ContentResponse) (tokenUsage, bool) {
	if resp == nil {
		return tokenUsage{}, false
	}
	for _, choice := range resp.Choices {
		if choice == nil || len(choice.GenerationInfo) == 0 {
			continue
		}
		candidates := providerUsageCandidates(choice.GenerationInfo)
		for _, candidate := range candidates {
			usage, ok := parseUsageMap(candidate)
			if !ok {
				continue
			}
			usage = mergeUsageCostFromCandidates(usage, candidates)
			usage.Source = "provider_native"
			if usage.hasCost() {
				usage.CostSource = "provider_native"
			}
			return usage, true
		}
	}
	return tokenUsage{}, false
}

func providerUsageCandidates(values map[string]any) []map[string]any {
	candidates := []map[string]any{values}
	for _, key := range []string{
		"usage",
		"usage_details",
		"token_usage",
		"usageDetails",
		"tokenUsage",
	} {
		if nested := mapFromAny(values[key]); len(nested) > 0 {
			candidates = append(candidates, nested)
		}
	}
	// Some providers expose costs separately; append these maps so parseUsageMap can merge them.
	for _, key := range []string{
		"cost",
		"cost_details",
		"costDetails",
		"pricing",
	} {
		if nested := mapFromAny(values[key]); len(nested) > 0 {
			candidates = append(candidates, nested)
		}
	}
	return candidates
}

// parseUsageMap извлекает prompt/completion/total токены из map с разными именами полей.
func parseUsageMap(values map[string]any) (tokenUsage, bool) {
	if len(values) == 0 {
		return tokenUsage{}, false
	}
	prompt, promptOK := firstInt(values, "PromptTokens", "prompt_tokens", "input_tokens")
	completion, completionOK := firstInt(values, "CompletionTokens", "completion_tokens", "output_tokens")
	total, totalOK := firstInt(values, "TotalTokens", "total_tokens")
	if !totalOK && (promptOK || completionOK) {
		total = prompt + completion
		totalOK = true
	}
	if !promptOK && totalOK && completionOK {
		prompt = total - completion
		promptOK = prompt >= 0
	}
	if !completionOK && totalOK && promptOK {
		completion = total - prompt
		completionOK = completion >= 0
	}
	if !promptOK || !completionOK || !totalOK {
		return tokenUsage{}, false
	}
	if prompt < 0 || completion < 0 || total < 0 {
		return tokenUsage{}, false
	}
	usage := tokenUsage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
	}

	inputCost, inputCostOK := firstFloat(
		values,
		"InputCost",
		"input_cost",
		"input",
		"prompt_cost",
		"prompt_cost_usd",
		"input_cost_usd",
	)
	outputCost, outputCostOK := firstFloat(
		values,
		"OutputCost",
		"output_cost",
		"output",
		"completion_cost",
		"completion_cost_usd",
		"output_cost_usd",
	)
	totalCost, totalCostOK := firstFloat(
		values,
		"TotalCost",
		"total_cost",
		"total",
		"cost",
		"cost_usd",
		"total_cost_usd",
	)
	if !totalCostOK && (inputCostOK || outputCostOK) {
		totalCost = inputCost + outputCost
		totalCostOK = true
	}
	if !inputCostOK && totalCostOK && outputCostOK {
		inputCost = totalCost - outputCost
		inputCostOK = inputCost >= 0
	}
	if !outputCostOK && totalCostOK && inputCostOK {
		outputCost = totalCost - inputCost
		outputCostOK = outputCost >= 0
	}
	if inputCostOK && outputCostOK && totalCostOK &&
		inputCost >= 0 && outputCost >= 0 && totalCost >= 0 {
		usage.InputCost = inputCost
		usage.OutputCost = outputCost
		usage.TotalCost = totalCost
		usage.CostSource = "provider_native"
	}
	return usage, true
}

func mapFromAny(raw any) map[string]any {
	values, ok := raw.(map[string]any)
	if !ok || len(values) == 0 {
		return nil
	}
	return values
}

func mergeUsageCostFromCandidates(usage tokenUsage, candidates []map[string]any) tokenUsage {
	if usage.hasCost() {
		return usage
	}
	for _, values := range candidates {
		inputCost, outputCost, totalCost, ok := costFromMap(values)
		if !ok {
			continue
		}
		usage.InputCost = inputCost
		usage.OutputCost = outputCost
		usage.TotalCost = totalCost
		usage.CostSource = "provider_native"
		return usage
	}
	return usage
}

func costFromMap(values map[string]any) (float64, float64, float64, bool) {
	if len(values) == 0 {
		return 0, 0, 0, false
	}
	inputCost, inputCostOK := firstFloat(
		values,
		"InputCost",
		"input_cost",
		"input",
		"prompt_cost",
		"prompt_cost_usd",
		"input_cost_usd",
	)
	outputCost, outputCostOK := firstFloat(
		values,
		"OutputCost",
		"output_cost",
		"output",
		"completion_cost",
		"completion_cost_usd",
		"output_cost_usd",
	)
	totalCost, totalCostOK := firstFloat(
		values,
		"TotalCost",
		"total_cost",
		"total",
		"cost",
		"cost_usd",
		"total_cost_usd",
	)
	if !inputCostOK && !outputCostOK && !totalCostOK {
		return 0, 0, 0, false
	}
	if !totalCostOK {
		totalCost = inputCost + outputCost
	}
	if !inputCostOK && outputCostOK {
		inputCost = maxFloat(totalCost-outputCost, 0)
	}
	if !outputCostOK && inputCostOK {
		outputCost = maxFloat(totalCost-inputCost, 0)
	}
	if inputCost < 0 || outputCost < 0 || totalCost < 0 {
		return 0, 0, 0, false
	}
	return inputCost, outputCost, totalCost, true
}

func maxFloat(v, minValue float64) float64 {
	if v < minValue {
		return minValue
	}
	return v
}

// firstInt возвращает первое корректно распарсенное целое по списку ключей.
func firstInt(values map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		if parsed, ok := parseInt(raw); ok {
			return parsed, true
		}
	}
	return 0, false
}

// firstFloat возвращает первое корректно распарсенное число по списку ключей.
func firstFloat(values map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		if parsed, ok := parseFloat(raw); ok {
			return parsed, true
		}
	}
	return 0, false
}

// parseInt преобразует динамическое значение в целое число токенов.
func parseInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func parseFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// emitTokenUsageMetrics публикует счётчики и гистограммы использования токенов.
func emitTokenUsageMetrics(metrics telemetry.Metrics, provider string, usage tokenUsage) {
	source := usage.Source
	if strings.TrimSpace(source) == "" {
		source = "estimated"
	}
	metrics.IncCounter("llm.tokens", int64(usage.PromptTokens), map[string]string{
		"provider": provider,
		"kind":     "prompt",
		"source":   source,
	})
	metrics.IncCounter("llm.tokens", int64(usage.CompletionTokens), map[string]string{
		"provider": provider,
		"kind":     "completion",
		"source":   source,
	})
	metrics.IncCounter("llm.tokens", int64(usage.TotalTokens), map[string]string{
		"provider": provider,
		"kind":     "total",
		"source":   source,
	})
	metrics.ObserveHistogram("llm.tokens.prompt", float64(usage.PromptTokens), map[string]string{
		"provider": provider,
		"source":   source,
	})
	metrics.ObserveHistogram("llm.tokens.completion", float64(usage.CompletionTokens), map[string]string{
		"provider": provider,
		"source":   source,
	})
	metrics.ObserveHistogram("llm.tokens.total", float64(usage.TotalTokens), map[string]string{
		"provider": provider,
		"source":   source,
	})
}

// estimateMessageTokens оценивает токены для всего набора сообщений.
func estimateMessageTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateTextTokens(msg.Content)
	}
	return total
}

// estimateTextTokens грубо оценивает число токенов по длине текста.
func estimateTextTokens(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return len(text)/4 + 1
}

// sanitizeLogValue обрезает слишком длинные строки и маскирует чувствительные токены.
func sanitizeLogValue(v string) string {
	if len(v) > 300 {
		v = v[:300]
	}
	return redact.Text(v)
}

// splitStreamChunks делит строку на последовательные фрагменты фиксированного размера.
func splitStreamChunks(text string, chunkSize int) []string {
	if text == "" {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = 48
	}
	parts := make([]string, 0, len(text)/chunkSize+1)
	for len(text) > 0 {
		if len(text) <= chunkSize {
			parts = append(parts, text)
			break
		}
		parts = append(parts, text[:chunkSize])
		text = text[chunkSize:]
	}
	return parts
}

// artifactMessages сериализует сообщения в компактный формат для debug-артефактов.
func artifactMessages(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		return "<messages_marshal_error>"
	}
	return string(raw)
}

// extractJSON извлекает и валидирует JSON-объект/массив из текстового ответа модели.
func extractJSON(text string) (json.RawMessage, error) {
	// trimmed удаляет внешние пробелы и используется как рабочая строка парсинга.
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, apperrors.New(apperrors.CodeValidation, "empty response", false)
	}

	if m := jsonBlockRegexp.FindStringSubmatch(trimmed); len(m) > 1 {
		trimmed = strings.TrimSpace(m[1])
	}

	// Некоторые OpenAI-совместимые модели (например в LM Studio) добавляют служебные префиксы
	// наподобие "<|channel|>final ...", после которых идет валидный JSON.
	if extracted, ok := extractJSONFromDecoratedText(trimmed); ok {
		trimmed = extracted
	}

	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return nil, apperrors.Wrap(apperrors.CodeValidation, "parse json", err, false)
	}
	return json.RawMessage(trimmed), nil
}

// extractJSONFromDecoratedText извлекает первый корректный JSON из строки с префиксами.
func extractJSONFromDecoratedText(text string) (string, bool) {
	objIdx := strings.Index(text, "{")
	arrIdx := strings.Index(text, "[")
	start := objIdx
	if start < 0 || (arrIdx >= 0 && arrIdx < start) {
		start = arrIdx
	}
	if start < 0 {
		return "", false
	}
	if start <= 0 {
		return "", false
	}
	candidate := strings.TrimSpace(text[start:])
	dec := json.NewDecoder(strings.NewReader(candidate))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return "", false
	}
	normalized, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(normalized), true
}

// acquire резервирует слот конкурентности для вызова LLM.
func (p *langChainProvider) acquire(ctx context.Context) (func(), error) {
	select {
	case p.limiter <- struct{}{}:
		return func() { <-p.limiter }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// classifyLLMError нормализует ошибку LLM-вызова в канонический AppError.
func classifyLLMError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled):
		return apperrors.Wrap(apperrors.CodeCanceled, "llm request canceled", err, false)
	case errors.Is(err, context.DeadlineExceeded):
		return apperrors.Wrap(apperrors.CodeTimeout, "llm request timeout", err, true)
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "rate limit"), strings.Contains(msg, "429"):
		return apperrors.Wrap(apperrors.CodeRateLimit, "llm rate limit", err, true)
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "401"), strings.Contains(msg, "api key"), strings.Contains(msg, "forbidden"):
		return apperrors.Wrap(apperrors.CodeAuth, "llm authentication failed", err, false)
	case strings.Contains(msg, "invalid request"), strings.Contains(msg, "400"), strings.Contains(msg, "bad request"):
		return apperrors.Wrap(apperrors.CodeBadRequest, "llm bad request", err, false)
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return apperrors.Wrap(apperrors.CodeTimeout, "llm timeout", err, true)
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "unavailable"), strings.Contains(msg, "502"), strings.Contains(msg, "503"), strings.Contains(msg, "eof"), strings.Contains(msg, "reset"):
		return apperrors.Wrap(apperrors.CodeTransient, "llm transient error", err, true)
	default:
		return apperrors.Wrap(apperrors.CodeTransient, "llm call failed", err, true)
	}
}

// circuitBreaker ограничивает количество подряд идущих ошибок перед временной блокировкой вызовов.
type circuitBreaker struct {
	// failureThreshold определяет число последовательных ошибок до открытия breaker.
	failureThreshold int
	// cooldown задаёт окно "остывания" после открытия breaker.
	cooldown time.Duration
	// mu защищает поля состояния breaker от гонок.
	mu sync.Mutex
	// consecutiveFailures хранит число последовательных неуспешных вызовов.
	consecutiveFailures int
	// openUntil задаёт момент времени, до которого breaker остаётся открытым.
	openUntil time.Time
}

// newCircuitBreaker создаёт breaker с безопасными дефолтами.
func newCircuitBreaker(failureThreshold int, cooldown time.Duration) *circuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if cooldown <= 0 {
		cooldown = 10 * time.Second
	}
	return &circuitBreaker{
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
	}
}

// Allow проверяет, открыт ли breaker в текущий момент.
func (c *circuitBreaker) Allow() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.openUntil) {
		return apperrors.New(apperrors.CodeTransient, "llm circuit breaker is open", true)
	}
	return nil
}

// MarkSuccess сбрасывает состояние breaker после успешного вызова.
func (c *circuitBreaker) MarkSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFailures = 0
	c.openUntil = time.Time{}
}

// MarkFailure учитывает ошибку и открывает breaker при достижении порога.
func (c *circuitBreaker) MarkFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFailures++
	if c.consecutiveFailures >= c.failureThreshold {
		c.openUntil = time.Now().Add(c.cooldown)
		c.consecutiveFailures = 0
	}
}

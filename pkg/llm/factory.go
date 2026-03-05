package llm

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// ModelClient создает конкретный executor модели для выбранного провайдера.
type ModelClient interface {
	Provider() string
	NewModel(cfg Config) (ChatExecutor, error)
}

// Реестр model client-ов по идентификатору провайдера.
var (
	// modelClientRegistryMu защищает реестр провайдеров от конкурентных модификаций.
	modelClientRegistryMu sync.RWMutex
	// modelClientRegistry хранит фабрики моделей по идентификатору провайдера.
	modelClientRegistry = map[string]ModelClient{
		ProviderOpenAI:     openAIClient{},
		ProviderOpenRouter: openRouterClient{},
		ProviderLMStudio:   lmStudioClient{},
		ProviderOllama:     ollamaClient{},
	}
)

// RegisterModelClient регистрирует или заменяет клиент модели для провайдера.
func RegisterModelClient(client ModelClient) error {
	if client == nil {
		return fmt.Errorf("model client is nil")
	}
	provider := strings.ToLower(strings.TrimSpace(client.Provider()))
	if provider == "" {
		return fmt.Errorf("model client provider is empty")
	}
	modelClientRegistryMu.Lock()
	defer modelClientRegistryMu.Unlock()
	modelClientRegistry[provider] = client
	return nil
}

// RegisteredProviders возвращает отсортированный список зарегистрированных провайдеров.
func RegisteredProviders() []string {
	modelClientRegistryMu.RLock()
	defer modelClientRegistryMu.RUnlock()
	out := make([]string, 0, len(modelClientRegistry))
	for provider := range modelClientRegistry {
		out = append(out, provider)
	}
	sort.Strings(out)
	return out
}

// NewProvider инициализирует LLM provider через реестр model client-ов.
func NewProvider(cfg Config, logger *slog.Logger, options ...ProviderOption) (Provider, error) {
	opts := providerOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}
		option.apply(&opts)
	}

	providerID := strings.ToLower(strings.TrimSpace(cfg.Provider))
	modelClientRegistryMu.RLock()
	client, ok := modelClientRegistry[providerID]
	modelClientRegistryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unsupported llm provider: %s", cfg.Provider)
	}

	model, err := client.NewModel(cfg)
	if err != nil {
		return nil, err
	}

	return newLangChainProvider(
		client.Provider(),
		cfg.Model,
		model,
		logger,
		time.Duration(cfg.TimeoutMs)*time.Millisecond,
		cfg.MaxRetries,
		time.Duration(cfg.RetryBaseMs)*time.Millisecond,
		cfg.MaxParallel,
		cfg.CircuitBreakerFailures,
		time.Duration(cfg.CircuitBreakerCooldownMs)*time.Millisecond,
		cfg.DisableJitter,
		time.Duration(cfg.CacheTTLms)*time.Millisecond,
		opts.cacheBackplane,
		opts.modelPrices,
	), nil
}

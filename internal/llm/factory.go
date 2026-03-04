package llm

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	appconfig "github.com/blanergol/agent-core/config"
)

type modelClient interface {
	Provider() string
	NewModel(cfg appconfig.LLMConfig) (chatExecutor, error)
}

var modelClientRegistry = map[string]modelClient{
	appconfig.ProviderOpenAI:     openAIClient{},
	appconfig.ProviderOpenRouter: openRouterClient{},
	appconfig.ProviderLMStudio:   lmStudioClient{},
	appconfig.ProviderOllama:     ollamaClient{},
}

// NewProvider initializes an LLM provider via model-client registry.
func NewProvider(cfg appconfig.LLMConfig, logger *slog.Logger, options ...ProviderOption) (Provider, error) {
	opts := providerOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}
		option.apply(&opts)
	}

	providerID := strings.ToLower(strings.TrimSpace(cfg.Provider))
	client, ok := modelClientRegistry[providerID]
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

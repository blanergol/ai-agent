package llm

import "github.com/blanergol/agent-core/internal/cache"

// providerOptions хранит внутренние опции инициализации провайдера.
type providerOptions struct {
	cacheBackplane cache.Backplane
	modelPrices    map[string]ModelPrice
}

// ModelPrice stores per-1M token pricing used for Langfuse cost details fallback.
type ModelPrice struct {
	InputPer1M  float64
	OutputPer1M float64
}

// ProviderOption настраивает дополнительные runtime-возможности LLM provider.
type ProviderOption interface {
	apply(*providerOptions)
}

// providerOptionFunc позволяет использовать функцию как реализацию ProviderOption.
type providerOptionFunc func(*providerOptions)

// apply применяет функциональную опцию к внутренней структуре настроек.
func (f providerOptionFunc) apply(opts *providerOptions) {
	f(opts)
}

// WithCacheBackplane подключает общий cache backplane для multi-instance runtime.
func WithCacheBackplane(backplane cache.Backplane) ProviderOption {
	return providerOptionFunc(func(opts *providerOptions) {
		opts.cacheBackplane = backplane
	})
}

// WithModelPrices configures per-model fallback pricing for Langfuse cost details.
func WithModelPrices(prices map[string]ModelPrice) ProviderOption {
	return providerOptionFunc(func(opts *providerOptions) {
		if len(prices) == 0 {
			opts.modelPrices = nil
			return
		}
		cloned := make(map[string]ModelPrice, len(prices))
		for model, price := range prices {
			cloned[model] = price
		}
		opts.modelPrices = cloned
	})
}

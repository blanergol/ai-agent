package llm

import (
	"fmt"

	"github.com/tmc/langchaingo/llms/ollama"
)

// ollamaClient реализует ModelClient для Ollama.
type ollamaClient struct{}

// Provider возвращает идентификатор провайдера Ollama.
func (ollamaClient) Provider() string {
	return ProviderOllama
}

// NewModel создает LangChain-клиент для Ollama.
func (ollamaClient) NewModel(cfg Config) (ChatExecutor, error) {
	opts := []ollama.Option{ollama.WithModel(cfg.Model)}
	if cfg.BaseURL != "" {
		opts = append(opts, ollama.WithServerURL(cfg.BaseURL))
	}
	model, err := ollama.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("init ollama client: %w", err)
	}
	return model, nil
}

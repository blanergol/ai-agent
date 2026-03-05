package llm

import (
	"fmt"

	"github.com/tmc/langchaingo/llms/openai"
)

// openAIClient реализует ModelClient для OpenAI-совместимых API.
type openAIClient struct{}

// Provider возвращает идентификатор провайдера OpenAI.
func (openAIClient) Provider() string {
	return ProviderOpenAI
}

// NewModel создает LangChain-клиент для OpenAI.
func (openAIClient) NewModel(cfg Config) (ChatExecutor, error) {
	opts := []openai.Option{openai.WithModel(cfg.Model)}
	if cfg.OpenAIAPIKey != "" {
		opts = append(opts, openai.WithToken(cfg.OpenAIAPIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.BaseURL))
	}
	model, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("init openai client: %w", err)
	}
	return model, nil
}

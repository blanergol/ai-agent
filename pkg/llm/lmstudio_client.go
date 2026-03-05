package llm

import (
	"fmt"

	"github.com/tmc/langchaingo/llms/openai"
)

// lmStudioClient реализует ModelClient для локального LM Studio.
type lmStudioClient struct{}

// Provider возвращает идентификатор провайдера LM Studio.
func (lmStudioClient) Provider() string {
	return ProviderLMStudio
}

// NewModel создает LangChain-клиент для LM Studio через OpenAI-совместимый API.
func (lmStudioClient) NewModel(cfg Config) (ChatExecutor, error) {
	opts := []openai.Option{
		openai.WithModel(cfg.Model),
		openai.WithBaseURL(cfg.BaseURL),
	}
	if cfg.OpenAIAPIKey != "" {
		opts = append(opts, openai.WithToken(cfg.OpenAIAPIKey))
	}
	model, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("init lmstudio client: %w", err)
	}
	return model, nil
}

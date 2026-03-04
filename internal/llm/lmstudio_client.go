package llm

import (
	"fmt"

	appconfig "github.com/blanergol/agent-core/config"
	"github.com/tmc/langchaingo/llms/openai"
)

type lmStudioClient struct{}

func (lmStudioClient) Provider() string {
	return appconfig.ProviderLMStudio
}

func (lmStudioClient) NewModel(cfg appconfig.LLMConfig) (chatExecutor, error) {
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

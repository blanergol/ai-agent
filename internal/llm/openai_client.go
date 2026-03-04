package llm

import (
	"fmt"

	appconfig "github.com/blanergol/agent-core/config"
	"github.com/tmc/langchaingo/llms/openai"
)

type openAIClient struct{}

func (openAIClient) Provider() string {
	return appconfig.ProviderOpenAI
}

func (openAIClient) NewModel(cfg appconfig.LLMConfig) (chatExecutor, error) {
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

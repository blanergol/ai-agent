package llm

import (
	"fmt"

	appconfig "github.com/blanergol/agent-core/config"
	"github.com/tmc/langchaingo/llms/ollama"
)

type ollamaClient struct{}

func (ollamaClient) Provider() string {
	return appconfig.ProviderOllama
}

func (ollamaClient) NewModel(cfg appconfig.LLMConfig) (chatExecutor, error) {
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

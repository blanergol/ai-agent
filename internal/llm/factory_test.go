package llm

import (
	"testing"

	appconfig "github.com/blanergol/agent-core/config"
)

// TestFactorySelectsProvider проверяет корректный выбор провайдера и имя результата.
func TestFactorySelectsProvider(t *testing.T) {
	// tests покрывает поддерживаемые типы провайдеров и разные базовые URL.
	tests := []struct {
		name     string
		provider string
		baseURL  string
	}{
		{name: "openai", provider: appconfig.ProviderOpenAI},
		{name: "openrouter", provider: appconfig.ProviderOpenRouter, baseURL: "https://openrouter.ai/api/v1"},
		{name: "ollama", provider: appconfig.ProviderOllama, baseURL: "http://localhost:11434"},
		{name: "lmstudio", provider: appconfig.ProviderLMStudio, baseURL: "http://localhost:1234/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// p - собранный провайдер для текущего сценария.
			p, err := NewProvider(appconfig.LLMConfig{
				Provider:     tt.provider,
				Model:        "test-model",
				BaseURL:      tt.baseURL,
				OpenAIAPIKey: "test-key",
				TimeoutMs:    1000,
				RetryBaseMs:  10,
			}, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Name() != tt.provider {
				t.Fatalf("provider name = %s, want %s", p.Name(), tt.provider)
			}
		})
	}
}

// TestFactoryAppliesDisableJitter проверяет передачу deterministic backoff-политики в провайдер.
func TestFactoryAppliesDisableJitter(t *testing.T) {
	p, err := NewProvider(appconfig.LLMConfig{
		Provider:      appconfig.ProviderOpenAI,
		Model:         "test-model",
		BaseURL:       "http://localhost:1234/v1",
		OpenAIAPIKey:  "test-key",
		TimeoutMs:     1000,
		RetryBaseMs:   10,
		DisableJitter: true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lp, ok := p.(*langChainProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *langChainProvider", p)
	}
	if !lp.disableJitter {
		t.Fatalf("disableJitter = %t, want true", lp.disableJitter)
	}
}

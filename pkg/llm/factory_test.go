package llm

import (
	"context"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

// TestFactorySelectsProvider проверяет корректный выбор провайдера и имя результата.
func TestFactorySelectsProvider(t *testing.T) {
	// tests покрывает поддерживаемые типы провайдеров и разные базовые URL.
	tests := []struct {
		name     string
		provider string
		baseURL  string
	}{
		{name: "openai", provider: ProviderOpenAI},
		{name: "openrouter", provider: ProviderOpenRouter, baseURL: "https://openrouter.ai/api/v1"},
		{name: "ollama", provider: ProviderOllama, baseURL: "http://localhost:11434"},
		{name: "lmstudio", provider: ProviderLMStudio, baseURL: "http://localhost:1234/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// p - собранный провайдер для текущего сценария.
			p, err := NewProvider(Config{
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
	p, err := NewProvider(Config{
		Provider:      ProviderOpenAI,
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

// customModelClient имитирует внешнюю реализацию ModelClient в unit-тесте фабрики.
type customModelClient struct{}

// Provider возвращает идентификатор тестового провайдера.
func (customModelClient) Provider() string { return "custom" }

// NewModel создает тестовый executor для проверки регистрации внешнего клиента.
func (customModelClient) NewModel(_ Config) (ChatExecutor, error) {
	return customExecutor{}, nil
}

// customExecutor эмулирует минимальный ответ модели в factory-тестах.
type customExecutor struct{}

// GenerateContent возвращает статический ответ без обращения к реальной модели.
func (customExecutor) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{Content: "ok"},
		},
	}, nil
}

// TestFactorySupportsRegisteredClient проверяет, что фабрика использует зарегистрированный ModelClient.
func TestFactorySupportsRegisteredClient(t *testing.T) {
	if err := RegisterModelClient(customModelClient{}); err != nil {
		t.Fatalf("register model client: %v", err)
	}
	p, err := NewProvider(Config{
		Provider: "custom",
		Model:    "custom-model",
	}, nil)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if p.Name() != "custom" {
		t.Fatalf("provider name = %s", p.Name())
	}
}

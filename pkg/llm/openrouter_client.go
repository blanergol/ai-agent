package llm

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/tmc/langchaingo/llms/openai"
)

// openRouterClient реализует ModelClient для OpenRouter.
type openRouterClient struct{}

// Provider возвращает идентификатор провайдера OpenRouter.
func (openRouterClient) Provider() string {
	return ProviderOpenRouter
}

// NewModel создает LangChain-клиент для OpenRouter с поддержкой служебных заголовков.
func (openRouterClient) NewModel(cfg Config) (ChatExecutor, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}

	token := strings.TrimSpace(cfg.OpenRouterAPIKey)
	if token == "" {
		token = strings.TrimSpace(cfg.OpenAIAPIKey)
	}

	opts := []openai.Option{
		openai.WithModel(cfg.Model),
		openai.WithBaseURL(baseURL),
	}
	if token != "" {
		opts = append(opts, openai.WithToken(token))
	}

	headers := map[string]string{}
	if referer := strings.TrimSpace(cfg.OpenRouterHTTPReferer); referer != "" {
		headers["HTTP-Referer"] = referer
	}
	if title := strings.TrimSpace(cfg.OpenRouterAppTitle); title != "" {
		headers["X-Title"] = title
	}
	if len(headers) > 0 {
		opts = append(opts, openai.WithHTTPClient(newHeaderInjectingHTTPClient(http.DefaultClient, headers)))
	}

	model, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("init openrouter client: %w", err)
	}
	return model, nil
}

// doer абстрагирует HTTP-клиент, выполняющий запросы.
type doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// headerInjectingHTTPClient добавляет обязательные заголовки OpenRouter к исходящим запросам.
type headerInjectingHTTPClient struct {
	base    doer
	headers map[string]string
}

// newHeaderInjectingHTTPClient создает HTTP-клиент-обертку, который добавляет заголовки.
func newHeaderInjectingHTTPClient(base doer, headers map[string]string) *headerInjectingHTTPClient {
	if base == nil {
		base = http.DefaultClient
	}
	cloned := make(map[string]string, len(headers))
	for k, v := range headers {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		cloned[k] = v
	}
	return &headerInjectingHTTPClient{base: base, headers: cloned}
}

// Do выполняет запрос, гарантируя присутствие настроенных заголовков.
func (c *headerInjectingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	for key, value := range c.headers {
		if cloned.Header.Get(key) == "" {
			cloned.Header.Set(key, value)
		}
	}
	return c.base.Do(cloned)
}

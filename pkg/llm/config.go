package llm

// Идентификаторы поддерживаемых LLM-провайдеров.
const (
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
	ProviderOllama     = "ollama"
	ProviderLMStudio   = "lmstudio"
)

// Config содержит параметры провайдера и runtime-настройки LLM-клиента.
type Config struct {
	Provider string
	Model    string
	BaseURL  string

	OpenAIAPIKey     string
	OpenRouterAPIKey string

	OpenRouterHTTPReferer string
	OpenRouterAppTitle    string

	TimeoutMs   int
	MaxRetries  int
	RetryBaseMs int
	MaxParallel int

	CircuitBreakerFailures   int
	CircuitBreakerCooldownMs int

	DisableJitter bool
	CacheTTLms    int
}

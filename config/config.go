package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blanergol/agent-core/pkg/helpers"
)

// Поддерживаемые идентификаторы LLM-провайдеров.
const (
	// ProviderOpenAI выбирает API-совместимого поставщика OpenAI.
	ProviderOpenAI = "openai"
	// ProviderOpenRouter выбирает OpenRouter API (OpenAI-compatible).
	ProviderOpenRouter = "openrouter"
	// ProviderOllama выбирает локальный Ollama-сервер.
	ProviderOllama = "ollama"
	// ProviderLMStudio выбирает OpenAI-совместимый endpoint LM Studio.
	ProviderLMStudio = "lmstudio"
)

// Config описывает полный runtime-конфиг приложения после загрузки env-переопределений.
type Config struct {
	// Mode задаёт профиль окружения (`dev|test|prod`).
	Mode string `mapstructure:"mode"`
	// LLM объединяет настройки провайдера и параметров генерации.
	LLM LLMConfig `mapstructure:"llm"`
	// Memory задаёт ограничения кратковременной и долговременной памяти.
	Memory MemoryConfig `mapstructure:"memory"`
	// Planner управляет политикой построения следующего действия.
	Planner PlannerConfig `mapstructure:"planner"`
	// Tools определяет политики и тайм-ауты для инструментов.
	Tools ToolsConfig `mapstructure:"tools"`
	// MCP описывает подключение внешних MCP-серверов.
	MCP MCPConfig `mapstructure:"mcp"`
	// State задаёт параметры хранения KV-состояния.
	State StateConfig `mapstructure:"state"`
	// Agent определяет параметры цикла выполнения шага.
	Agent AgentConfig `mapstructure:"agent"`
	// Guardrails задаёт лимиты безопасности на уровне выполнения.
	Guardrails GuardrailsConfig `mapstructure:"guardrails"`
	// Output задаёт правила валидации финального ответа агента.
	Output OutputConfig `mapstructure:"output"`
	// Auth описывает способ получения пользовательского контекста.
	Auth AuthConfig `mapstructure:"auth"`
	// Logging включает/отключает подробный уровень логов.
	Logging LoggingConfig `mapstructure:"logging"`
	// WebUI управляет встроенным одностраничным интерфейсом тестирования агента.
	WebUI WebUIConfig `mapstructure:"web_ui"`
	// Langfuse задаёт параметры экспорта трассировок и артефактов в Langfuse.
	Langfuse LangfuseConfig `mapstructure:"langfuse"`
	// Skills содержит список активируемых наборов инструментов.
	Skills []string `mapstructure:"skills"`
	// Bundle controls opt-in internal Bundle pipeline/business extensions.
	Bundle BundleConfig `mapstructure:"bundle"`
}

// LLMConfig объединяет параметры подключения к LLM и генерации ответов.
type LLMConfig struct {
	// Provider определяет конкретную реализацию клиента LLM.
	Provider string `mapstructure:"provider"`
	// Model задаёт идентификатор модели у выбранного провайдера.
	Model string `mapstructure:"model"`
	// BaseURL позволяет направить запросы в кастомный endpoint.
	BaseURL string `mapstructure:"base_url"`
	// OpenAIAPIKey хранит секрет для доступа к OpenAI-совместимому API.
	OpenAIAPIKey string `mapstructure:"openai_api_key"`
	// OpenRouterAPIKey хранит секрет для доступа к OpenRouter API.
	OpenRouterAPIKey string `mapstructure:"openrouter_api_key"`
	// OpenRouterHTTPReferer задает HTTP-Referer заголовок для OpenRouter.
	OpenRouterHTTPReferer string `mapstructure:"openrouter_http_referer"`
	// OpenRouterAppTitle задает X-Title заголовок для OpenRouter.
	OpenRouterAppTitle string `mapstructure:"openrouter_app_title"`
	// Temperature управляет степенью вариативности ответов.
	Temperature float64 `mapstructure:"temperature"`
	// TopP задаёт top-p сэмплирование для модели (если поддерживается провайдером).
	TopP float64 `mapstructure:"top_p"`
	// Seed задаёт детерминируемый seed (если поддерживается провайдером).
	Seed int `mapstructure:"seed"`
	// MaxOutputTokens ограничивает размер генерируемого ответа модели.
	MaxOutputTokens int `mapstructure:"max_output_tokens"`
	// TimeoutMs ограничивает длительность одного обращения к LLM.
	TimeoutMs int `mapstructure:"timeout_ms"`
	// MaxRetries задаёт число повторных попыток при ошибках сети/сервиса.
	MaxRetries int `mapstructure:"max_retries"`
	// RetryBaseMs определяет базовую задержку для экспоненциального backoff.
	RetryBaseMs int `mapstructure:"retry_base_ms"`
	// MaxParallel ограничивает количество одновременных запросов к LLM.
	MaxParallel int `mapstructure:"max_parallel"`
	// CircuitBreakerFailures задаёт порог последовательных сбоев до открытия circuit breaker.
	CircuitBreakerFailures int `mapstructure:"circuit_breaker_failures"`
	// CircuitBreakerCooldownMs задаёт время "остывания" circuit breaker.
	CircuitBreakerCooldownMs int `mapstructure:"circuit_breaker_cooldown_ms"`
	// DisableJitter отключает случайную компоненту backoff для воспроизводимых тестов.
	DisableJitter bool `mapstructure:"disable_jitter"`
	// CacheTTLms задаёт TTL кэша chat-ответов LLM (0 отключает кэш).
	CacheTTLms int `mapstructure:"cache_ttl_ms"`
}

// MemoryConfig задаёт лимиты и стратегию формирования контекста памяти.
type MemoryConfig struct {
	// ShortTermMaxMessages ограничивает длину окна диалога в кратковременной памяти.
	ShortTermMaxMessages int `mapstructure:"short_term_max_messages"`
	// RecallTopK определяет сколько записей поднимать из долговременной памяти.
	RecallTopK int `mapstructure:"recall_top_k"`
	// TokenBudget ограничивает объём контекста, отправляемого в LLM.
	TokenBudget int `mapstructure:"token_budget"`
}

// PlannerConfig управляет повторами и ограничениями цикла планирования.
type PlannerConfig struct {
	// MaxSteps ограничивает число итераций планирования/выполнения.
	MaxSteps int `mapstructure:"max_steps"`
	// MaxPlanningRetries задаёт допустимые повторные вызовы планировщика.
	MaxPlanningRetries int `mapstructure:"max_planning_retries"`
	// ActionJSONRetries определяет число попыток исправить некорректный JSON-действий.
	ActionJSONRetries int `mapstructure:"action_json_retries"`
}

// ToolsConfig определяет политики исполнения и сетевые лимиты инструментов.
type ToolsConfig struct {
	// Allowlist задаёт явный список разрешённых инструментов.
	Allowlist []string `mapstructure:"allowlist"`
	// Denylist принудительно блокирует инструменты даже при наличии в allowlist.
	Denylist []string `mapstructure:"denylist"`
	// DefaultTimeoutMs - тайм-аут по умолчанию для вызовов инструментов.
	DefaultTimeoutMs int `mapstructure:"default_timeout_ms"`
	// MaxExecutionRetries задаёт число повторных попыток выполнения инструмента.
	MaxExecutionRetries int `mapstructure:"max_execution_retries"`
	// RetryBaseMs задаёт базовую задержку экспоненциального backoff между retry-попытками.
	RetryBaseMs int `mapstructure:"retry_base_ms"`
	// MaxParallel ограничивает число одновременных вызовов инструментов.
	MaxParallel int `mapstructure:"max_parallel"`
	// DedupTTLms задаёт TTL дедупликации mutating-инструментов.
	DedupTTLms int `mapstructure:"dedup_ttl_ms"`
	// MaxOutputBytes ограничивает размер stdout/результата инструмента.
	MaxOutputBytes int `mapstructure:"max_output_bytes"`
	// HTTPAllowDomains перечисляет домены, доступные для `http.get`.
	HTTPAllowDomains []string `mapstructure:"http_allow_domains"`
	// HTTPMaxBodyBytes ограничивает объём загружаемого HTTP-ответа.
	HTTPMaxBodyBytes int64 `mapstructure:"http_max_body_bytes"`
	// HTTPTimeoutMs ограничивает длительность сетевого запроса `http.get`.
	HTTPTimeoutMs int `mapstructure:"http_timeout_ms"`
	// HTTPReadCacheTTLms задаёт TTL кэша read-only вызовов `http.get`.
	HTTPReadCacheTTLms int `mapstructure:"http_read_cache_ttl_ms"`
}

// MCPConfig включает и настраивает импорт инструментов с MCP-серверов.
type MCPConfig struct {
	// Enabled включает импорт инструментов из внешних MCP-серверов.
	Enabled bool `mapstructure:"enabled"`
	// Servers задаёт список серверов, из которых нужно импортировать инструменты.
	Servers []MCPServerConfig `mapstructure:"servers"`
}

// MCPServerConfig описывает подключение к одному внешнему MCP-серверу.
type MCPServerConfig struct {
	// Name используется в префиксе инструментов `mcp.<name>.*`.
	Name string `json:"name" mapstructure:"name"`
	// BaseURL задаёт корневой HTTP endpoint MCP-сервера.
	BaseURL string `json:"base_url" mapstructure:"base_url"`
	// Token содержит bearer-токен для авторизации к MCP-серверу.
	Token string `json:"token" mapstructure:"token"`
	// OAuth21 включает OAuth 2.1 client credentials для MCP-сервера.
	OAuth21 OAuth21ClientConfig `json:"oauth2_1" mapstructure:"oauth2_1"`
	// Enabled позволяет точечно включать/выключать конкретный сервер.
	Enabled bool `json:"enabled" mapstructure:"enabled"`
}

// OAuth21ClientConfig описывает OAuth 2.1 client credentials-конфигурацию исходящих запросов.
type OAuth21ClientConfig struct {
	// Enabled включает OAuth 2.1 client credentials для конкретного MCP-сервера.
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// IssuerURL задаёт issuer URL для OIDC discovery (`/.well-known/openid-configuration`).
	IssuerURL string `json:"issuer_url" mapstructure:"issuer_url"`
	// TokenURL задаёт явный token endpoint (если discovery не используется).
	TokenURL string `json:"token_url" mapstructure:"token_url"`
	// ClientID задаёт OAuth client_id.
	ClientID string `json:"client_id" mapstructure:"client_id"`
	// ClientSecret задаёт OAuth client_secret.
	ClientSecret string `json:"client_secret" mapstructure:"client_secret"`
	// Audience задаёт целевой audience параметр token endpoint (если поддерживается IdP).
	Audience string `json:"audience" mapstructure:"audience"`
	// Scopes задаёт запрашиваемые OAuth scopes.
	Scopes []string `json:"scopes" mapstructure:"scopes"`
	// AuthMethod задаёт способ аутентификации клиента: client_secret_basic|client_secret_post|none.
	AuthMethod string `json:"auth_method" mapstructure:"auth_method"`
	// ClockSkewSec задаёт запас в секундах перед фактическим истечением токена.
	ClockSkewSec int `json:"clock_skew_sec" mapstructure:"clock_skew_sec"`
	// AllowInsecureHTTP разрешает http endpoint-ы (обычно только для localhost/dev).
	AllowInsecureHTTP bool `json:"allow_insecure_http" mapstructure:"allow_insecure_http"`
}

// OAuth21ResourceServerConfig describes OAuth 2.1 resource-server verification settings.
type OAuth21ResourceServerConfig struct {
	// Enabled включает проверку bearer access token для входящих HTTP-запросов.
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// IssuerURL задаёт issuer URL для проверки токена и OIDC discovery.
	IssuerURL string `json:"issuer_url" mapstructure:"issuer_url"`
	// JWKSURL задаёт явный JWKS endpoint (если discovery не используется).
	JWKSURL string `json:"jwks_url" mapstructure:"jwks_url"`
	// Audience задаёт ожидаемую аудиторию access token.
	Audience string `json:"audience" mapstructure:"audience"`
	// RequiredScopes задаёт минимальный набор scopes, обязательный для вызова API.
	RequiredScopes []string `json:"required_scopes" mapstructure:"required_scopes"`
	// AllowedAlgs ограничивает допустимые JWT подписи (например RS256, ES256).
	AllowedAlgs []string `json:"allowed_algs" mapstructure:"allowed_algs"`
	// ClockSkewSec задаёт допустимый сдвиг времени при проверке exp/nbf/iat.
	ClockSkewSec int `json:"clock_skew_sec" mapstructure:"clock_skew_sec"`
	// SubjectClaim задаёт claim, который считается user subject (по умолчанию `sub`).
	SubjectClaim string `json:"subject_claim" mapstructure:"subject_claim"`
	// ScopeClaim задаёт claim со scope-значениями (по умолчанию `scope`).
	ScopeClaim string `json:"scope_claim" mapstructure:"scope_claim"`
	// AllowInsecureHTTP разрешает http endpoints issuer/jwks (обычно только localhost/dev).
	AllowInsecureHTTP bool `json:"allow_insecure_http" mapstructure:"allow_insecure_http"`
}

// StateConfig управляет персистентностью state и общим cache backplane.
type StateConfig struct {
	// PersistPath указывает путь к JSON-файлу для персистентного KV-хранилища.
	PersistPath string `mapstructure:"persist_path"`
	// CacheBackplaneDir задаёт директорию file-backed cache backplane для multi-instance runtime.
	CacheBackplaneDir string `mapstructure:"cache_backplane_dir"`
	// TimeoutMs ограничивает операции snapshot persistence/read через context timeout.
	TimeoutMs int `mapstructure:"timeout_ms"`
}

// AgentConfig содержит параметры поведения основного цикла агента.
type AgentConfig struct {
	// MaxStepDurationMs ограничивает время одного шага выполнения агента.
	MaxStepDurationMs int `mapstructure:"max_step_duration_ms"`
	// ContinueOnToolError позволяет агенту продолжать цикл при ошибке tool-вызова.
	ContinueOnToolError bool `mapstructure:"continue_on_tool_error"`
	// ToolErrorMode задаёт глобальный режим деградации по tool-ошибкам: fail|continue.
	ToolErrorMode string `mapstructure:"tool_error_mode"`
	// ToolErrorFallback задаёт per-tool override режима деградации: {"tool.name":"fail|continue"}.
	ToolErrorFallback map[string]string `mapstructure:"tool_error_fallback"`
	// MaxInputChars ограничивает длину пользовательского входа одного запуска.
	MaxInputChars int `mapstructure:"max_input_chars"`
	// Deterministic включает воспроизводимый режим runtime (stable ids, deterministic behavior).
	Deterministic bool `mapstructure:"deterministic"`
	// MCPEnrichmentSources задаёт детерминированные MCP-вызовы для фазы ENRICH_CONTEXT.
	MCPEnrichmentSources []MCPEnrichmentSource `mapstructure:"mcp_enrichment_sources"`
	// RequireToolApproval включает human approval для mutating tool calls.
	RequireToolApproval bool `mapstructure:"require_tool_approval"`
	// ApprovalAutoApproveTools задаёт CSV-список tool names, которые можно выполнять без approval.
	ApprovalAutoApproveTools []string `mapstructure:"approval_auto_approve_tools"`
}

// MCPEnrichmentSource describes one deterministic enrichment tool call.
type MCPEnrichmentSource struct {
	Name     string          `json:"name" mapstructure:"name"`
	ToolName string          `json:"tool_name" mapstructure:"tool_name"`
	Args     json.RawMessage `json:"args" mapstructure:"args"`
	Required bool            `json:"required" mapstructure:"required"`
}

// GuardrailsConfig задаёт hard-лимиты безопасности во время выполнения.
type GuardrailsConfig struct {
	// MaxSteps ограничивает общее число шагов в одной задаче.
	MaxSteps int `mapstructure:"max_steps"`
	// MaxToolCalls ограничивает количество вызовов инструментов.
	MaxToolCalls int `mapstructure:"max_tool_calls"`
	// MaxTimeMs ограничивает суммарную длительность обработки запроса.
	MaxTimeMs int `mapstructure:"max_time_ms"`
	// MaxToolOutputBytes ограничивает размер вывода одного инструмента.
	MaxToolOutputBytes int `mapstructure:"max_tool_output_bytes"`
}

// OutputConfig определяет правила валидации финального ответа агента.
type OutputConfig struct {
	// MaxChars ограничивает длину финального ответа агента.
	MaxChars int `mapstructure:"max_chars"`
	// ForbiddenSubstrings задаёт список запрещённых фрагментов в финальном ответе.
	ForbiddenSubstrings []string `mapstructure:"forbidden_substrings"`
	// ValidationRetries задаёт число повторных попыток исправить невалидный финальный ответ.
	ValidationRetries int `mapstructure:"validation_retries"`
	// JSONSchema задаёт строгий структурный контракт финального ответа.
	JSONSchema string `mapstructure:"json_schema"`
}

// AuthConfig описывает способ извлечения user subject из входного запроса.
type AuthConfig struct {
	// UserAuthHeader задаёт имя HTTP-заголовка с user subject.
	UserAuthHeader string `mapstructure:"user_auth_header"`
	// OAuth21 включает OAuth 2.1 валидацию bearer access token на входящем HTTP API.
	OAuth21 OAuth21ResourceServerConfig `mapstructure:"oauth2_1"`
}

// LoggingConfig задаёт уровень диагностической и трассировочной телеметрии.
type LoggingConfig struct {
	// Debug включает расширенный уровень логирования.
	Debug bool `mapstructure:"debug"`
	// VerboseTracing включает span-события и внутренние технические логи трассировки.
	VerboseTracing bool `mapstructure:"verbose_tracing"`
	// DebugArtifacts включает сохранение debug-артефактов prompt/response/state.
	DebugArtifacts bool `mapstructure:"debug_artifacts"`
	// DebugArtifactsMaxChars ограничивает размер payload одного debug-артефакта.
	DebugArtifactsMaxChars int `mapstructure:"debug_artifacts_max_chars"`
}

// WebUIConfig задаёт включение встроенного web-интерфейса для ручного тестирования агента.
type WebUIConfig struct {
	// Enabled включает страницу `/` и `/ui` с формой отправки запросов в `/v1/agent/run`.
	Enabled bool `mapstructure:"enabled"`
}

// LangfuseConfig задаёт конфигурацию экспорта telemetry данных в Langfuse.
type LangfuseConfig struct {
	// Enabled включает экспорт telemetry в Langfuse.
	Enabled bool `mapstructure:"enabled"`
	// Host задаёт базовый URL Langfuse API, например `http://langfuse-web:3000`.
	Host string `mapstructure:"host"`
	// PublicKey содержит публичный ключ проекта Langfuse.
	PublicKey string `mapstructure:"public_key"`
	// SecretKey содержит секретный ключ проекта Langfuse.
	SecretKey string `mapstructure:"secret_key"`
	// TimeoutMs ограничивает тайм-аут отправки OTLP-запросов.
	TimeoutMs int `mapstructure:"timeout_ms"`
	// ServiceName задаёт service.name в ресурсных атрибутах OTEL.
	ServiceName string `mapstructure:"service_name"`
	// ServiceVersion задаёт service.version в ресурсных атрибутах OTEL.
	ServiceVersion string `mapstructure:"service_version"`
	// Environment задаёт deployment.environment в ресурсных атрибутах OTEL.
	Environment string `mapstructure:"environment"`
	// ModelPrices задаёт fallback-цены (USD за 1M токенов) для расчёта cost_details в Langfuse.
	ModelPrices map[string]LangfuseModelPrice `mapstructure:"model_prices"`
}

// LangfuseModelPrice defines fallback pricing for one model.
type LangfuseModelPrice struct {
	// InputPer1M задаёт стоимость входных токенов (USD за 1M токенов).
	InputPer1M float64 `json:"input_per_1m" mapstructure:"input_per_1m"`
	// OutputPer1M задаёт стоимость выходных токенов (USD за 1M токенов).
	OutputPer1M float64 `json:"output_per_1m" mapstructure:"output_per_1m"`
}

// BundleConfig controls opt-in internal bundle runtime extensions.
type BundleConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// DefaultConfig возвращает безопасные значения по умолчанию для локального запуска.
func DefaultConfig() Config {
	return Config{
		Mode: "dev",
		LLM: LLMConfig{
			Provider:                 ProviderOpenAI,
			Model:                    "gpt-4o-mini",
			Temperature:              0,
			TopP:                     1,
			Seed:                     0,
			MaxOutputTokens:          1024,
			TimeoutMs:                20000,
			MaxRetries:               2,
			RetryBaseMs:              300,
			MaxParallel:              4,
			CircuitBreakerFailures:   5,
			CircuitBreakerCooldownMs: 10000,
			DisableJitter:            false,
			CacheTTLms:               30000,
		},
		Memory: MemoryConfig{
			ShortTermMaxMessages: 20,
			RecallTopK:           5,
			TokenBudget:          2048,
		},
		Planner: PlannerConfig{
			MaxSteps:           8,
			MaxPlanningRetries: 2,
			ActionJSONRetries:  2,
		},
		Tools: ToolsConfig{
			Allowlist:           []string{"time.now", "kv.put", "kv.get"},
			DefaultTimeoutMs:    5000,
			MaxExecutionRetries: 1,
			RetryBaseMs:         200,
			MaxParallel:         16,
			DedupTTLms:          int((10 * time.Minute).Milliseconds()),
			MaxOutputBytes:      64 * 1024,
			HTTPAllowDomains:    []string{"example.com"},
			HTTPMaxBodyBytes:    64 * 1024,
			HTTPTimeoutMs:       5000,
			HTTPReadCacheTTLms:  30000,
		},
		MCP: MCPConfig{Enabled: false},
		State: StateConfig{
			CacheBackplaneDir: "",
			TimeoutMs:         1500,
		},
		Guardrails: GuardrailsConfig{
			MaxSteps:           8,
			MaxToolCalls:       8,
			MaxTimeMs:          int((2 * time.Minute).Milliseconds()),
			MaxToolOutputBytes: 64 * 1024,
		},
		Output: OutputConfig{
			MaxChars:          8000,
			ValidationRetries: 1,
			JSONSchema:        `{"type":"string","minLength":1}`,
		},
		Agent: AgentConfig{
			MaxStepDurationMs:   20000,
			ContinueOnToolError: true,
			ToolErrorMode:       "continue",
			ToolErrorFallback:   map[string]string{},
			MaxInputChars:       8000,
			Deterministic:       false,
			RequireToolApproval: true,
		},
		Auth: AuthConfig{
			UserAuthHeader: "X-User-Sub",
			OAuth21: OAuth21ResourceServerConfig{
				Enabled:      false,
				AllowedAlgs:  []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"},
				ClockSkewSec: 60,
				SubjectClaim: "sub",
				ScopeClaim:   "scope",
			},
		},
		Logging: LoggingConfig{
			Debug:                  false,
			VerboseTracing:         false,
			DebugArtifacts:         false,
			DebugArtifactsMaxChars: 2000,
		},
		WebUI: WebUIConfig{
			Enabled: false,
		},
		Langfuse: LangfuseConfig{
			Enabled:        false,
			Host:           "",
			PublicKey:      "",
			SecretKey:      "",
			TimeoutMs:      10000,
			ServiceName:    "agent-core",
			ServiceVersion: "",
			Environment:    "dev",
			ModelPrices:    map[string]LangfuseModelPrice{},
		},
		Skills: []string{"ops"},
		Bundle: BundleConfig{
			Enabled: false,
		},
	}
}

// Load собирает финальную конфигурацию из дефолтов, переменных окружения и производных значений.
func Load() (Config, error) {
	// cfg последовательно дополняется источниками конфигурации.
	cfg := DefaultConfig()

	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	overrideSecrets(&cfg)
	applyDerivedDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyEnv применяет явные переопределения из переменных окружения.
func applyEnv(cfg *Config) error {
	cfg.Mode = helpers.EnvString("AGENT_CORE_MODE", cfg.Mode)

	cfg.LLM.Provider = helpers.EnvString("AGENT_CORE_LLM_PROVIDER", cfg.LLM.Provider)
	cfg.LLM.Model = helpers.EnvString("AGENT_CORE_LLM_MODEL", cfg.LLM.Model)
	cfg.LLM.BaseURL = helpers.EnvString("AGENT_CORE_LLM_BASE_URL", cfg.LLM.BaseURL)
	cfg.LLM.OpenAIAPIKey = helpers.EnvString("AGENT_CORE_LLM_OPENAI_API_KEY", cfg.LLM.OpenAIAPIKey)
	cfg.LLM.OpenRouterAPIKey = helpers.EnvString("AGENT_CORE_LLM_OPENROUTER_API_KEY", cfg.LLM.OpenRouterAPIKey)
	cfg.LLM.OpenRouterHTTPReferer = helpers.EnvString("AGENT_CORE_LLM_OPENROUTER_HTTP_REFERER", cfg.LLM.OpenRouterHTTPReferer)
	cfg.LLM.OpenRouterAppTitle = helpers.EnvString("AGENT_CORE_LLM_OPENROUTER_APP_TITLE", cfg.LLM.OpenRouterAppTitle)

	if v, ok, err := helpers.EnvFloat64("AGENT_CORE_LLM_TEMPERATURE"); err != nil {
		return err
	} else if ok {
		cfg.LLM.Temperature = v
	}
	if v, ok, err := helpers.EnvFloat64("AGENT_CORE_LLM_TOP_P"); err != nil {
		return err
	} else if ok {
		cfg.LLM.TopP = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LLM_SEED"); err != nil {
		return err
	} else if ok {
		cfg.LLM.Seed = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LLM_MAX_OUTPUT_TOKENS"); err != nil {
		return err
	} else if ok {
		cfg.LLM.MaxOutputTokens = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LLM_TIMEOUT_MS"); err != nil {
		return err
	} else if ok {
		cfg.LLM.TimeoutMs = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LLM_MAX_RETRIES"); err != nil {
		return err
	} else if ok {
		cfg.LLM.MaxRetries = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LLM_RETRY_BASE_MS"); err != nil {
		return err
	} else if ok {
		cfg.LLM.RetryBaseMs = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LLM_MAX_PARALLEL"); err != nil {
		return err
	} else if ok {
		cfg.LLM.MaxParallel = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LLM_CIRCUIT_BREAKER_FAILURES"); err != nil {
		return err
	} else if ok {
		cfg.LLM.CircuitBreakerFailures = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LLM_CIRCUIT_BREAKER_COOLDOWN_MS"); err != nil {
		return err
	} else if ok {
		cfg.LLM.CircuitBreakerCooldownMs = v
	}
	if v, ok, err := helpers.EnvBool("AGENT_CORE_LLM_DISABLE_JITTER"); err != nil {
		return err
	} else if ok {
		cfg.LLM.DisableJitter = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LLM_CACHE_TTL_MS"); err != nil {
		return err
	} else if ok {
		cfg.LLM.CacheTTLms = v
	}

	if v, ok, err := helpers.EnvInt("AGENT_CORE_MEMORY_SHORT_TERM_MAX_MESSAGES"); err != nil {
		return err
	} else if ok {
		cfg.Memory.ShortTermMaxMessages = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_MEMORY_RECALL_TOP_K"); err != nil {
		return err
	} else if ok {
		cfg.Memory.RecallTopK = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_MEMORY_TOKEN_BUDGET"); err != nil {
		return err
	} else if ok {
		cfg.Memory.TokenBudget = v
	}

	if v, ok, err := helpers.EnvInt("AGENT_CORE_PLANNER_MAX_STEPS"); err != nil {
		return err
	} else if ok {
		cfg.Planner.MaxSteps = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_PLANNER_MAX_PLANNING_RETRIES"); err != nil {
		return err
	} else if ok {
		cfg.Planner.MaxPlanningRetries = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_PLANNER_ACTION_JSON_RETRIES"); err != nil {
		return err
	} else if ok {
		cfg.Planner.ActionJSONRetries = v
	}

	if v, ok := helpers.EnvCSV("AGENT_CORE_TOOLS_ALLOWLIST"); ok {
		cfg.Tools.Allowlist = v
	}
	if v, ok := helpers.EnvCSV("AGENT_CORE_TOOLS_DENYLIST"); ok {
		cfg.Tools.Denylist = v
	}
	if v, ok := helpers.EnvCSV("AGENT_CORE_TOOLS_HTTP_ALLOW_DOMAINS"); ok {
		cfg.Tools.HTTPAllowDomains = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_TOOLS_DEFAULT_TIMEOUT_MS"); err != nil {
		return err
	} else if ok {
		cfg.Tools.DefaultTimeoutMs = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_TOOLS_MAX_EXECUTION_RETRIES"); err != nil {
		return err
	} else if ok {
		cfg.Tools.MaxExecutionRetries = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_TOOLS_RETRY_BASE_MS"); err != nil {
		return err
	} else if ok {
		cfg.Tools.RetryBaseMs = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_TOOLS_MAX_PARALLEL"); err != nil {
		return err
	} else if ok {
		cfg.Tools.MaxParallel = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_TOOLS_DEDUP_TTL_MS"); err != nil {
		return err
	} else if ok {
		cfg.Tools.DedupTTLms = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_TOOLS_MAX_OUTPUT_BYTES"); err != nil {
		return err
	} else if ok {
		cfg.Tools.MaxOutputBytes = v
	}
	if v, ok, err := helpers.EnvInt64("AGENT_CORE_TOOLS_HTTP_MAX_BODY_BYTES"); err != nil {
		return err
	} else if ok {
		cfg.Tools.HTTPMaxBodyBytes = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_TOOLS_HTTP_TIMEOUT_MS"); err != nil {
		return err
	} else if ok {
		cfg.Tools.HTTPTimeoutMs = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_TOOLS_HTTP_READ_CACHE_TTL_MS"); err != nil {
		return err
	} else if ok {
		cfg.Tools.HTTPReadCacheTTLms = v
	}

	if v, ok, err := helpers.EnvBool("AGENT_CORE_MCP_ENABLED"); err != nil {
		return err
	} else if ok {
		cfg.MCP.Enabled = v
	}
	// raw хранит строковое описание серверов MCP (JSON или `key=value` формат).
	if raw, ok := os.LookupEnv("AGENT_CORE_MCP_SERVERS"); ok {
		servers, err := parseMCPServers(raw)
		if err != nil {
			return err
		}
		cfg.MCP.Servers = servers
	}

	cfg.State.PersistPath = helpers.EnvString("AGENT_CORE_STATE_PERSIST_PATH", cfg.State.PersistPath)
	cfg.State.CacheBackplaneDir = helpers.EnvString("AGENT_CORE_STATE_CACHE_BACKPLANE_DIR", cfg.State.CacheBackplaneDir)
	if v, ok, err := helpers.EnvInt("AGENT_CORE_STATE_TIMEOUT_MS"); err != nil {
		return err
	} else if ok {
		cfg.State.TimeoutMs = v
	}

	if v, ok, err := helpers.EnvInt("AGENT_CORE_AGENT_MAX_STEP_DURATION_MS"); err != nil {
		return err
	} else if ok {
		cfg.Agent.MaxStepDurationMs = v
	}
	if v, ok, err := helpers.EnvBool("AGENT_CORE_AGENT_CONTINUE_ON_TOOL_ERROR"); err != nil {
		return err
	} else if ok {
		cfg.Agent.ContinueOnToolError = v
	}
	cfg.Agent.ToolErrorMode = helpers.EnvString("AGENT_CORE_AGENT_TOOL_ERROR_MODE", cfg.Agent.ToolErrorMode)
	if raw, ok := os.LookupEnv("AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK"); ok {
		parsed, err := parseToolErrorFallback(raw)
		if err != nil {
			return err
		}
		cfg.Agent.ToolErrorFallback = parsed
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_AGENT_MAX_INPUT_CHARS"); err != nil {
		return err
	} else if ok {
		cfg.Agent.MaxInputChars = v
	}
	if v, ok, err := helpers.EnvBool("AGENT_CORE_AGENT_DETERMINISTIC"); err != nil {
		return err
	} else if ok {
		cfg.Agent.Deterministic = v
	}
	if v, ok, err := helpers.EnvBool("AGENT_CORE_AGENT_REQUIRE_TOOL_APPROVAL"); err != nil {
		return err
	} else if ok {
		cfg.Agent.RequireToolApproval = v
	}
	if v, ok := helpers.EnvCSV("AGENT_CORE_AGENT_APPROVAL_AUTO_APPROVE_TOOLS"); ok {
		cfg.Agent.ApprovalAutoApproveTools = v
	}
	if raw, ok := os.LookupEnv("AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES"); ok {
		sources, err := parseMCPEnrichmentSources(raw)
		if err != nil {
			return err
		}
		cfg.Agent.MCPEnrichmentSources = sources
	}

	if v, ok, err := helpers.EnvInt("AGENT_CORE_GUARDRAILS_MAX_STEPS"); err != nil {
		return err
	} else if ok {
		cfg.Guardrails.MaxSteps = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_GUARDRAILS_MAX_TOOL_CALLS"); err != nil {
		return err
	} else if ok {
		cfg.Guardrails.MaxToolCalls = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_GUARDRAILS_MAX_TIME_MS"); err != nil {
		return err
	} else if ok {
		cfg.Guardrails.MaxTimeMs = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_GUARDRAILS_MAX_TOOL_OUTPUT_BYTES"); err != nil {
		return err
	} else if ok {
		cfg.Guardrails.MaxToolOutputBytes = v
	}

	if v, ok, err := helpers.EnvInt("AGENT_CORE_OUTPUT_MAX_CHARS"); err != nil {
		return err
	} else if ok {
		cfg.Output.MaxChars = v
	}
	if v, ok := helpers.EnvCSV("AGENT_CORE_OUTPUT_FORBIDDEN_SUBSTRINGS"); ok {
		cfg.Output.ForbiddenSubstrings = v
	}
	cfg.Output.JSONSchema = helpers.EnvString("AGENT_CORE_OUTPUT_JSON_SCHEMA", cfg.Output.JSONSchema)
	if v, ok, err := helpers.EnvInt("AGENT_CORE_OUTPUT_VALIDATION_RETRIES"); err != nil {
		return err
	} else if ok {
		cfg.Output.ValidationRetries = v
	}

	cfg.Auth.UserAuthHeader = helpers.EnvString("AGENT_CORE_AUTH_USER_AUTH_HEADER", cfg.Auth.UserAuthHeader)
	if v, ok, err := helpers.EnvBool("AGENT_CORE_AUTH_OAUTH2_1_ENABLED"); err != nil {
		return err
	} else if ok {
		cfg.Auth.OAuth21.Enabled = v
	}
	cfg.Auth.OAuth21.IssuerURL = helpers.EnvString("AGENT_CORE_AUTH_OAUTH2_1_ISSUER_URL", cfg.Auth.OAuth21.IssuerURL)
	cfg.Auth.OAuth21.JWKSURL = helpers.EnvString("AGENT_CORE_AUTH_OAUTH2_1_JWKS_URL", cfg.Auth.OAuth21.JWKSURL)
	cfg.Auth.OAuth21.Audience = helpers.EnvString("AGENT_CORE_AUTH_OAUTH2_1_AUDIENCE", cfg.Auth.OAuth21.Audience)
	if v, ok := helpers.EnvCSV("AGENT_CORE_AUTH_OAUTH2_1_REQUIRED_SCOPES"); ok {
		cfg.Auth.OAuth21.RequiredScopes = v
	}
	if v, ok := helpers.EnvCSV("AGENT_CORE_AUTH_OAUTH2_1_ALLOWED_ALGS"); ok {
		cfg.Auth.OAuth21.AllowedAlgs = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_AUTH_OAUTH2_1_CLOCK_SKEW_SEC"); err != nil {
		return err
	} else if ok {
		cfg.Auth.OAuth21.ClockSkewSec = v
	}
	cfg.Auth.OAuth21.SubjectClaim = helpers.EnvString("AGENT_CORE_AUTH_OAUTH2_1_SUBJECT_CLAIM", cfg.Auth.OAuth21.SubjectClaim)
	cfg.Auth.OAuth21.ScopeClaim = helpers.EnvString("AGENT_CORE_AUTH_OAUTH2_1_SCOPE_CLAIM", cfg.Auth.OAuth21.ScopeClaim)
	if v, ok, err := helpers.EnvBool("AGENT_CORE_AUTH_OAUTH2_1_ALLOW_INSECURE_HTTP"); err != nil {
		return err
	} else if ok {
		cfg.Auth.OAuth21.AllowInsecureHTTP = v
	}

	if v, ok, err := helpers.EnvBool("AGENT_CORE_LOGGING_DEBUG"); err != nil {
		return err
	} else if ok {
		cfg.Logging.Debug = v
	}
	if v, ok, err := helpers.EnvBool("AGENT_CORE_LOGGING_VERBOSE_TRACING"); err != nil {
		return err
	} else if ok {
		cfg.Logging.VerboseTracing = v
	}
	if v, ok, err := helpers.EnvBool("AGENT_CORE_LOGGING_DEBUG_ARTIFACTS"); err != nil {
		return err
	} else if ok {
		cfg.Logging.DebugArtifacts = v
	}
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LOGGING_DEBUG_ARTIFACTS_MAX_CHARS"); err != nil {
		return err
	} else if ok {
		cfg.Logging.DebugArtifactsMaxChars = v
	}
	if v, ok, err := helpers.EnvBool("AGENT_CORE_WEB_UI_ENABLED"); err != nil {
		return err
	} else if ok {
		cfg.WebUI.Enabled = v
	}
	if v, ok, err := helpers.EnvBool("AGENT_CORE_LANGFUSE_ENABLED"); err != nil {
		return err
	} else if ok {
		cfg.Langfuse.Enabled = v
	}
	cfg.Langfuse.Host = helpers.EnvString("AGENT_CORE_LANGFUSE_HOST", cfg.Langfuse.Host)
	cfg.Langfuse.PublicKey = helpers.EnvString("AGENT_CORE_LANGFUSE_PUBLIC_KEY", cfg.Langfuse.PublicKey)
	cfg.Langfuse.SecretKey = helpers.EnvString("AGENT_CORE_LANGFUSE_SECRET_KEY", cfg.Langfuse.SecretKey)
	if v, ok, err := helpers.EnvInt("AGENT_CORE_LANGFUSE_TIMEOUT_MS"); err != nil {
		return err
	} else if ok {
		cfg.Langfuse.TimeoutMs = v
	}
	cfg.Langfuse.ServiceName = helpers.EnvString("AGENT_CORE_LANGFUSE_SERVICE_NAME", cfg.Langfuse.ServiceName)
	cfg.Langfuse.ServiceVersion = helpers.EnvString("AGENT_CORE_LANGFUSE_SERVICE_VERSION", cfg.Langfuse.ServiceVersion)
	cfg.Langfuse.Environment = helpers.EnvString("AGENT_CORE_LANGFUSE_ENVIRONMENT", cfg.Langfuse.Environment)
	if raw, ok := os.LookupEnv("AGENT_CORE_LANGFUSE_MODEL_PRICES"); ok {
		modelPrices, err := parseLangfuseModelPrices(raw)
		if err != nil {
			return err
		}
		cfg.Langfuse.ModelPrices = modelPrices
	}

	if v, ok := helpers.EnvCSV("AGENT_CORE_SKILLS"); ok {
		cfg.Skills = v
	}
	if v, ok, err := helpers.EnvBool("AGENT_CORE_BUNDLE_ENABLED"); err != nil {
		return err
	} else if ok {
		cfg.Bundle.Enabled = v
	}

	return nil
}

// overrideSecrets подтягивает секреты из общеиспользуемых переменных окружения.
func overrideSecrets(cfg *Config) {
	if cfg.LLM.OpenAIAPIKey == "" {
		cfg.LLM.OpenAIAPIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if cfg.LLM.OpenRouterAPIKey == "" {
		cfg.LLM.OpenRouterAPIKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	if cfg.LLM.OpenRouterAPIKey == "" {
		cfg.LLM.OpenRouterAPIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if cfg.LLM.OpenRouterAPIKey == "" {
		cfg.LLM.OpenRouterAPIKey = cfg.LLM.OpenAIAPIKey
	}
	if strings.TrimSpace(cfg.Langfuse.Host) == "" {
		cfg.Langfuse.Host = strings.TrimSpace(os.Getenv("LANGFUSE_HOST"))
	}
	if strings.TrimSpace(cfg.Langfuse.PublicKey) == "" {
		cfg.Langfuse.PublicKey = strings.TrimSpace(os.Getenv("LANGFUSE_PUBLIC_KEY"))
	}
	if strings.TrimSpace(cfg.Langfuse.SecretKey) == "" {
		cfg.Langfuse.SecretKey = strings.TrimSpace(os.Getenv("LANGFUSE_SECRET_KEY"))
	}
	for i := range cfg.MCP.Servers {
		if strings.TrimSpace(cfg.MCP.Servers[i].Token) != "" {
			// explicit token has priority over fallback env.
		} else {
			// envName строится по имени сервера, чтобы выбрать токен вида `MCP_TOKEN_<SERVER>`.
			envName := "MCP_TOKEN_" + strings.ToUpper(strings.ReplaceAll(cfg.MCP.Servers[i].Name, "-", "_"))
			cfg.MCP.Servers[i].Token = strings.TrimSpace(os.Getenv(envName))
		}
		if !cfg.MCP.Servers[i].OAuth21.Enabled {
			continue
		}
		prefix := strings.ToUpper(strings.ReplaceAll(cfg.MCP.Servers[i].Name, "-", "_"))
		if strings.TrimSpace(cfg.MCP.Servers[i].OAuth21.ClientSecret) == "" {
			cfg.MCP.Servers[i].OAuth21.ClientSecret = strings.TrimSpace(os.Getenv("MCP_OAUTH_CLIENT_SECRET_" + prefix))
		}
		if strings.TrimSpace(cfg.MCP.Servers[i].OAuth21.ClientID) == "" {
			cfg.MCP.Servers[i].OAuth21.ClientID = strings.TrimSpace(os.Getenv("MCP_OAUTH_CLIENT_ID_" + prefix))
		}
	}
}

// applyDerivedDefaults заполняет взаимосвязанные поля, если они не заданы явно.
func applyDerivedDefaults(cfg *Config) {
	if strings.EqualFold(strings.TrimSpace(cfg.LLM.Provider), ProviderOpenRouter) &&
		strings.TrimSpace(cfg.LLM.BaseURL) == "" {
		cfg.LLM.BaseURL = "https://openrouter.ai/api/v1"
	}
	if cfg.Guardrails.MaxSteps <= 0 {
		cfg.Guardrails.MaxSteps = cfg.Planner.MaxSteps
	}
	if cfg.Guardrails.MaxToolOutputBytes <= 0 {
		cfg.Guardrails.MaxToolOutputBytes = cfg.Tools.MaxOutputBytes
	}
	if cfg.Agent.MaxStepDurationMs <= 0 {
		cfg.Agent.MaxStepDurationMs = cfg.LLM.TimeoutMs
	}
	if cfg.Agent.MaxInputChars <= 0 {
		cfg.Agent.MaxInputChars = 8000
	}
	if cfg.Tools.HTTPTimeoutMs <= 0 {
		cfg.Tools.HTTPTimeoutMs = cfg.Tools.DefaultTimeoutMs
	}
	if cfg.Tools.HTTPReadCacheTTLms < 0 {
		cfg.Tools.HTTPReadCacheTTLms = 0
	}
	if cfg.Tools.MaxExecutionRetries < 0 {
		cfg.Tools.MaxExecutionRetries = 0
	}
	if cfg.Tools.RetryBaseMs <= 0 {
		cfg.Tools.RetryBaseMs = 200
	}
	if cfg.Tools.MaxParallel <= 0 {
		cfg.Tools.MaxParallel = 16
	}
	if cfg.Tools.DedupTTLms <= 0 {
		cfg.Tools.DedupTTLms = int((10 * time.Minute).Milliseconds())
	}
	if cfg.Memory.TokenBudget <= 0 {
		cfg.Memory.TokenBudget = 2048
	}
	if cfg.State.TimeoutMs <= 0 {
		cfg.State.TimeoutMs = 1500
	}
	if strings.TrimSpace(cfg.State.PersistPath) == "" && !strings.EqualFold(strings.TrimSpace(cfg.Mode), "test") {
		cfg.State.PersistPath = filepath.Join("data", "agent-state.sqlite")
	}
	if strings.TrimSpace(cfg.State.CacheBackplaneDir) == "" && strings.TrimSpace(cfg.State.PersistPath) != "" {
		cfg.State.CacheBackplaneDir = filepath.Join(filepath.Dir(cfg.State.PersistPath), "cache-backplane")
	}
	if cfg.LLM.MaxParallel <= 0 {
		cfg.LLM.MaxParallel = 4
	}
	if cfg.LLM.CircuitBreakerFailures <= 0 {
		cfg.LLM.CircuitBreakerFailures = 5
	}
	if cfg.LLM.CircuitBreakerCooldownMs <= 0 {
		cfg.LLM.CircuitBreakerCooldownMs = 10000
	}
	if cfg.LLM.CacheTTLms < 0 {
		cfg.LLM.CacheTTLms = 0
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), "test") {
		cfg.Agent.Deterministic = true
		cfg.LLM.DisableJitter = true
	}
	if strings.TrimSpace(cfg.Agent.ToolErrorMode) == "" {
		if cfg.Agent.ContinueOnToolError {
			cfg.Agent.ToolErrorMode = "continue"
		} else {
			cfg.Agent.ToolErrorMode = "fail"
		}
	}
	if cfg.Agent.ToolErrorFallback == nil {
		cfg.Agent.ToolErrorFallback = map[string]string{}
	}
	cfg.Agent.ApprovalAutoApproveTools = normalizeUniqueStrings(cfg.Agent.ApprovalAutoApproveTools)
	cfg.Auth.OAuth21.RequiredScopes = normalizeUniqueStrings(cfg.Auth.OAuth21.RequiredScopes)
	cfg.Auth.OAuth21.AllowedAlgs = normalizeUniqueStrings(cfg.Auth.OAuth21.AllowedAlgs)
	if len(cfg.Auth.OAuth21.AllowedAlgs) == 0 {
		cfg.Auth.OAuth21.AllowedAlgs = []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"}
	}
	if cfg.Auth.OAuth21.ClockSkewSec <= 0 {
		cfg.Auth.OAuth21.ClockSkewSec = 60
	}
	if strings.TrimSpace(cfg.Auth.OAuth21.SubjectClaim) == "" {
		cfg.Auth.OAuth21.SubjectClaim = "sub"
	}
	if strings.TrimSpace(cfg.Auth.OAuth21.ScopeClaim) == "" {
		cfg.Auth.OAuth21.ScopeClaim = "scope"
	}
	for i := range cfg.MCP.Servers {
		cfg.MCP.Servers[i].OAuth21.Scopes = normalizeUniqueStrings(cfg.MCP.Servers[i].OAuth21.Scopes)
		if strings.TrimSpace(cfg.MCP.Servers[i].OAuth21.AuthMethod) == "" {
			cfg.MCP.Servers[i].OAuth21.AuthMethod = "client_secret_basic"
		}
		if cfg.MCP.Servers[i].OAuth21.ClockSkewSec <= 0 {
			cfg.MCP.Servers[i].OAuth21.ClockSkewSec = 30
		}
	}
	if cfg.Output.MaxChars <= 0 {
		cfg.Output.MaxChars = 8000
	}
	if cfg.Output.ValidationRetries < 0 {
		cfg.Output.ValidationRetries = 0
	}
	if cfg.Logging.DebugArtifactsMaxChars <= 0 {
		cfg.Logging.DebugArtifactsMaxChars = 2000
	}
	if cfg.Langfuse.TimeoutMs <= 0 {
		cfg.Langfuse.TimeoutMs = 10000
	}
	if strings.TrimSpace(cfg.Langfuse.ServiceName) == "" {
		cfg.Langfuse.ServiceName = "agent-core"
	}
	if strings.TrimSpace(cfg.Langfuse.Environment) == "" {
		cfg.Langfuse.Environment = cfg.Mode
	}
	if len(cfg.Langfuse.ModelPrices) == 0 {
		cfg.Langfuse.ModelPrices = map[string]LangfuseModelPrice{}
	} else {
		normalized := make(map[string]LangfuseModelPrice, len(cfg.Langfuse.ModelPrices))
		for model, price := range cfg.Langfuse.ModelPrices {
			key := strings.ToLower(strings.TrimSpace(model))
			if key == "" {
				continue
			}
			normalized[key] = price
		}
		cfg.Langfuse.ModelPrices = normalized
	}
	if !cfg.Langfuse.Enabled &&
		strings.TrimSpace(cfg.Langfuse.Host) != "" &&
		strings.TrimSpace(cfg.Langfuse.PublicKey) != "" &&
		strings.TrimSpace(cfg.Langfuse.SecretKey) != "" {
		cfg.Langfuse.Enabled = true
	}
}

// validate проверяет, что критичные параметры конфигурации принадлежат поддерживаемому набору.
func validate(cfg *Config) error {
	switch strings.ToLower(strings.TrimSpace(cfg.LLM.Provider)) {
	case ProviderOpenAI, ProviderOpenRouter, ProviderOllama, ProviderLMStudio:
	default:
		return fmt.Errorf("invalid AGENT_CORE_LLM_PROVIDER: %s", cfg.LLM.Provider)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "dev", "test", "prod":
	default:
		return fmt.Errorf("invalid AGENT_CORE_MODE: %s", cfg.Mode)
	}
	if strings.EqualFold(cfg.LLM.Provider, ProviderOpenAI) &&
		strings.TrimSpace(cfg.LLM.OpenAIAPIKey) == "" &&
		strings.TrimSpace(cfg.LLM.BaseURL) == "" &&
		!strings.EqualFold(strings.TrimSpace(cfg.Mode), "test") {
		return fmt.Errorf("missing required OPENAI api key for provider=%s", ProviderOpenAI)
	}
	if strings.EqualFold(cfg.LLM.Provider, ProviderOpenRouter) &&
		strings.TrimSpace(cfg.LLM.OpenRouterAPIKey) == "" &&
		!strings.EqualFold(strings.TrimSpace(cfg.Mode), "test") {
		return fmt.Errorf("missing required OPENROUTER api key for provider=%s", ProviderOpenRouter)
	}
	if cfg.Langfuse.Enabled {
		if strings.TrimSpace(cfg.Langfuse.Host) == "" {
			return fmt.Errorf("AGENT_CORE_LANGFUSE_HOST is required when AGENT_CORE_LANGFUSE_ENABLED=true")
		}
		if strings.TrimSpace(cfg.Langfuse.PublicKey) == "" {
			return fmt.Errorf("AGENT_CORE_LANGFUSE_PUBLIC_KEY is required when AGENT_CORE_LANGFUSE_ENABLED=true")
		}
		if strings.TrimSpace(cfg.Langfuse.SecretKey) == "" {
			return fmt.Errorf("AGENT_CORE_LANGFUSE_SECRET_KEY is required when AGENT_CORE_LANGFUSE_ENABLED=true")
		}
	}
	for model, price := range cfg.Langfuse.ModelPrices {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("AGENT_CORE_LANGFUSE_MODEL_PRICES contains empty model key")
		}
		if price.InputPer1M < 0 || price.OutputPer1M < 0 {
			return fmt.Errorf("AGENT_CORE_LANGFUSE_MODEL_PRICES[%s] must be >= 0", model)
		}
	}
	if cfg.LLM.TimeoutMs <= 0 {
		return fmt.Errorf("AGENT_CORE_LLM_TIMEOUT_MS must be > 0")
	}
	if cfg.Agent.MaxStepDurationMs <= 0 {
		return fmt.Errorf("AGENT_CORE_AGENT_MAX_STEP_DURATION_MS must be > 0")
	}
	if cfg.Agent.MaxInputChars <= 0 {
		return fmt.Errorf("AGENT_CORE_AGENT_MAX_INPUT_CHARS must be > 0")
	}
	if cfg.Tools.DefaultTimeoutMs <= 0 {
		return fmt.Errorf("AGENT_CORE_TOOLS_DEFAULT_TIMEOUT_MS must be > 0")
	}
	if cfg.Memory.ShortTermMaxMessages <= 0 {
		return fmt.Errorf("AGENT_CORE_MEMORY_SHORT_TERM_MAX_MESSAGES must be > 0")
	}
	if cfg.Memory.TokenBudget <= 0 {
		return fmt.Errorf("AGENT_CORE_MEMORY_TOKEN_BUDGET must be > 0")
	}
	if cfg.State.TimeoutMs <= 0 {
		return fmt.Errorf("AGENT_CORE_STATE_TIMEOUT_MS must be > 0")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Agent.ToolErrorMode)) {
	case "fail", "continue":
	default:
		return fmt.Errorf("AGENT_CORE_AGENT_TOOL_ERROR_MODE must be one of: fail|continue")
	}
	for tool, mode := range cfg.Agent.ToolErrorFallback {
		if strings.TrimSpace(tool) == "" {
			return fmt.Errorf("AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK contains empty tool name")
		}
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "fail", "continue":
		default:
			return fmt.Errorf("AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK[%s] must be fail|continue", tool)
		}
	}
	for idx, source := range cfg.Agent.MCPEnrichmentSources {
		if strings.TrimSpace(source.ToolName) == "" {
			return fmt.Errorf("AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES[%d].tool_name must be non-empty", idx)
		}
		if len(source.Args) > 0 && !json.Valid(source.Args) {
			return fmt.Errorf("AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES[%d].args must be valid JSON", idx)
		}
	}
	if cfg.Auth.OAuth21.Enabled {
		if strings.TrimSpace(cfg.Auth.OAuth21.Audience) == "" {
			return fmt.Errorf("AGENT_CORE_AUTH_OAUTH2_1_AUDIENCE is required when AGENT_CORE_AUTH_OAUTH2_1_ENABLED=true")
		}
		if strings.TrimSpace(cfg.Auth.OAuth21.IssuerURL) == "" && strings.TrimSpace(cfg.Auth.OAuth21.JWKSURL) == "" {
			return fmt.Errorf("AGENT_CORE_AUTH_OAUTH2_1_ISSUER_URL or AGENT_CORE_AUTH_OAUTH2_1_JWKS_URL is required when AGENT_CORE_AUTH_OAUTH2_1_ENABLED=true")
		}
		if err := validateOAuthURL(cfg.Auth.OAuth21.IssuerURL, cfg.Auth.OAuth21.AllowInsecureHTTP, "AGENT_CORE_AUTH_OAUTH2_1_ISSUER_URL"); err != nil {
			return err
		}
		if err := validateOAuthURL(cfg.Auth.OAuth21.JWKSURL, cfg.Auth.OAuth21.AllowInsecureHTTP, "AGENT_CORE_AUTH_OAUTH2_1_JWKS_URL"); err != nil {
			return err
		}
		if len(cfg.Auth.OAuth21.AllowedAlgs) == 0 {
			return fmt.Errorf("AGENT_CORE_AUTH_OAUTH2_1_ALLOWED_ALGS must contain at least one algorithm")
		}
	}
	for idx, server := range cfg.MCP.Servers {
		if strings.TrimSpace(server.Name) == "" {
			return fmt.Errorf("AGENT_CORE_MCP_SERVERS[%d].name must be non-empty", idx)
		}
		if strings.TrimSpace(server.BaseURL) == "" {
			return fmt.Errorf("AGENT_CORE_MCP_SERVERS[%d].base_url must be non-empty", idx)
		}
		if !server.OAuth21.Enabled {
			continue
		}
		if strings.TrimSpace(server.Token) != "" {
			return fmt.Errorf("AGENT_CORE_MCP_SERVERS[%d] cannot use both token and oauth2_1", idx)
		}
		if strings.TrimSpace(server.OAuth21.ClientID) == "" {
			return fmt.Errorf("AGENT_CORE_MCP_SERVERS[%d].oauth2_1.client_id must be non-empty", idx)
		}
		authMethod := strings.ToLower(strings.TrimSpace(server.OAuth21.AuthMethod))
		switch authMethod {
		case "client_secret_basic", "client_secret_post", "none":
		default:
			return fmt.Errorf(
				"AGENT_CORE_MCP_SERVERS[%d].oauth2_1.auth_method must be one of: client_secret_basic|client_secret_post|none",
				idx,
			)
		}
		if authMethod != "none" && strings.TrimSpace(server.OAuth21.ClientSecret) == "" {
			return fmt.Errorf("AGENT_CORE_MCP_SERVERS[%d].oauth2_1.client_secret must be non-empty for auth_method=%s", idx, authMethod)
		}
		if strings.TrimSpace(server.OAuth21.IssuerURL) == "" && strings.TrimSpace(server.OAuth21.TokenURL) == "" {
			return fmt.Errorf("AGENT_CORE_MCP_SERVERS[%d].oauth2_1 requires issuer_url or token_url", idx)
		}
		if err := validateOAuthURL(
			server.OAuth21.IssuerURL,
			server.OAuth21.AllowInsecureHTTP,
			fmt.Sprintf("AGENT_CORE_MCP_SERVERS[%d].oauth2_1.issuer_url", idx),
		); err != nil {
			return err
		}
		if err := validateOAuthURL(
			server.OAuth21.TokenURL,
			server.OAuth21.AllowInsecureHTTP,
			fmt.Sprintf("AGENT_CORE_MCP_SERVERS[%d].oauth2_1.token_url", idx),
		); err != nil {
			return err
		}
	}
	return nil
}

// parseMCPServers парсит список серверов MCP из JSON или компактного текстового формата.
func parseMCPServers(raw string) ([]MCPServerConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []MCPServerConfig{}, nil
	}
	if strings.HasPrefix(raw, "[") {
		// servers хранит десериализованный список серверов из JSON-массива.
		var servers []MCPServerConfig
		if err := json.Unmarshal([]byte(raw), &servers); err != nil {
			return nil, fmt.Errorf("parse AGENT_CORE_MCP_SERVERS as JSON: %w", err)
		}
		return servers, nil
	}

	// parts разделяет строку на отдельные описания серверов по `;`.
	parts := strings.Split(raw, ";")
	// servers аккумулирует итоговые структуры для подключения к MCP.
	servers := make([]MCPServerConfig, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// server инициализируется включённым, чтобы не требовать `enabled=true` каждый раз.
		server := MCPServerConfig{Enabled: true}
		for _, token := range strings.Split(part, ",") {
			// kv разбивает пару `key=value` на имя поля и значение.
			kv := strings.SplitN(strings.TrimSpace(token), "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid MCP server token: %q", token)
			}
			// key нормализуется для предсказуемого сравнения и поддержки регистра.
			key := strings.TrimSpace(strings.ToLower(kv[0]))
			// value хранит уже обрезанное значение текущего поля сервера.
			value := strings.TrimSpace(kv[1])
			switch key {
			case "name":
				server.Name = value
			case "base_url":
				server.BaseURL = value
			case "token":
				server.Token = value
			case "enabled":
				enabled, err := strconv.ParseBool(value)
				if err != nil {
					return nil, fmt.Errorf("invalid MCP enabled value %q: %w", value, err)
				}
				server.Enabled = enabled
			case "oauth2_1_enabled", "oauth21_enabled", "oauth_enabled":
				enabled, err := strconv.ParseBool(value)
				if err != nil {
					return nil, fmt.Errorf("invalid MCP oauth enabled value %q: %w", value, err)
				}
				server.OAuth21.Enabled = enabled
			case "oauth2_1_issuer_url", "oauth21_issuer_url":
				server.OAuth21.IssuerURL = value
			case "oauth2_1_token_url", "oauth21_token_url":
				server.OAuth21.TokenURL = value
			case "oauth2_1_client_id", "oauth21_client_id":
				server.OAuth21.ClientID = value
			case "oauth2_1_client_secret", "oauth21_client_secret":
				server.OAuth21.ClientSecret = value
			case "oauth2_1_audience", "oauth21_audience":
				server.OAuth21.Audience = value
			case "oauth2_1_scopes", "oauth21_scopes":
				server.OAuth21.Scopes = parseScopeList(value)
			case "oauth2_1_auth_method", "oauth21_auth_method":
				server.OAuth21.AuthMethod = value
			case "oauth2_1_clock_skew_sec", "oauth21_clock_skew_sec":
				skewSec, err := strconv.Atoi(value)
				if err != nil {
					return nil, fmt.Errorf("invalid MCP oauth clock_skew_sec value %q: %w", value, err)
				}
				server.OAuth21.ClockSkewSec = skewSec
			case "oauth2_1_allow_insecure_http", "oauth21_allow_insecure_http":
				allowInsecure, err := strconv.ParseBool(value)
				if err != nil {
					return nil, fmt.Errorf("invalid MCP oauth allow_insecure_http value %q: %w", value, err)
				}
				server.OAuth21.AllowInsecureHTTP = allowInsecure
			default:
				return nil, fmt.Errorf("unsupported MCP server key: %s", key)
			}
		}
		if server.Name == "" || server.BaseURL == "" {
			return nil, fmt.Errorf("MCP server requires name and base_url: %q", part)
		}
		servers = append(servers, server)
	}
	return servers, nil
}

// parseMCPEnrichmentSources parses deterministic MCP enrichment sources from JSON array.
func parseMCPEnrichmentSources(raw string) ([]MCPEnrichmentSource, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []MCPEnrichmentSource{}, nil
	}
	var sources []MCPEnrichmentSource
	if err := json.Unmarshal([]byte(raw), &sources); err != nil {
		return nil, fmt.Errorf("parse AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES as JSON: %w", err)
	}
	for i := range sources {
		sources[i].Name = strings.TrimSpace(sources[i].Name)
		sources[i].ToolName = strings.TrimSpace(sources[i].ToolName)
		if len(sources[i].Args) == 0 {
			sources[i].Args = json.RawMessage("{}")
		}
		if !json.Valid(sources[i].Args) {
			return nil, fmt.Errorf("invalid MCP enrichment args at index %d", i)
		}
	}
	return sources, nil
}

// parseToolErrorFallback разбирает fallback-правила tool_error_fallback из JSON или CSV-формата.
func parseToolErrorFallback(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}
	if strings.HasPrefix(raw, "{") {
		out := map[string]string{}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return nil, fmt.Errorf("parse AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK as JSON: %w", err)
		}
		return out, nil
	}

	out := map[string]string{}
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		kv := strings.SplitN(token, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK token: %q", token)
		}
		tool := strings.TrimSpace(kv[0])
		mode := strings.TrimSpace(kv[1])
		if tool == "" {
			return nil, fmt.Errorf("invalid AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK token: %q", token)
		}
		out[tool] = mode
	}
	return out, nil
}

// parseLangfuseModelPrices parses JSON object with per-model pricing.
// Bundle:
// {"openai/gpt-4o-mini":{"input_per_1m":0.15,"output_per_1m":0.6}}
func parseLangfuseModelPrices(raw string) (map[string]LangfuseModelPrice, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]LangfuseModelPrice{}, nil
	}
	out := map[string]LangfuseModelPrice{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("parse AGENT_CORE_LANGFUSE_MODEL_PRICES as JSON: %w", err)
	}
	normalized := make(map[string]LangfuseModelPrice, len(out))
	for model, price := range out {
		key := strings.ToLower(strings.TrimSpace(model))
		if key == "" {
			return nil, fmt.Errorf("AGENT_CORE_LANGFUSE_MODEL_PRICES contains empty model key")
		}
		normalized[key] = LangfuseModelPrice{
			InputPer1M:  price.InputPer1M,
			OutputPer1M: price.OutputPer1M,
		}
	}
	return normalized, nil
}

func parseScopeList(raw string) []string {
	normalized := strings.NewReplacer("|", " ", ",", " ", ";", " ").Replace(strings.TrimSpace(raw))
	return normalizeUniqueStrings(strings.Fields(normalized))
}

func normalizeUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validateOAuthURL(raw string, allowInsecure bool, label string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s parse error: %w", label, err)
	}
	if strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("%s must include scheme and host", label)
	}
	if !strings.EqualFold(parsed.Scheme, "https") && !strings.EqualFold(parsed.Scheme, "http") {
		return fmt.Errorf("%s scheme must be https or http", label)
	}
	if strings.EqualFold(parsed.Scheme, "http") && !allowInsecure && !isLocalhostHost(parsed.Host) {
		return fmt.Errorf("%s must use https", label)
	}
	return nil
}

func isLocalhostHost(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if value, _, err := net.SplitHostPort(hostport); err == nil {
		host = value
	}
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

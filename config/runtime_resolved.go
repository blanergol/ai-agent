package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/guardrails"
	"github.com/blanergol/agent-core/pkg/llm"
	"github.com/blanergol/agent-core/pkg/output"
	"github.com/blanergol/agent-core/pkg/planner"
	"github.com/blanergol/agent-core/pkg/retry"
	"github.com/blanergol/agent-core/pkg/telemetry"
	toolkit "github.com/blanergol/agent-core/pkg/tools"
)

// RuntimeOverrides задает CLI/entrypoint-переопределения поверх загруженной конфигурации.
type RuntimeOverrides struct {
	Provider string
	Model    string
	Debug    *bool
}

// MemoryRuntimeConfig хранит готовые параметры менеджера памяти.
type MemoryRuntimeConfig struct {
	ShortTermMaxMessages int
	RecallTopK           int
	TokenBudget          int
}

// MCPRuntimeConfig хранит готовые параметры интеграции MCP-серверов.
type MCPRuntimeConfig struct {
	Enabled bool
	Servers []MCPServerConfig
}

// OutputRuntimeConfig хранит готовую конфигурацию валидации финального ответа.
type OutputRuntimeConfig struct {
	Policy            output.Policy
	Schema            string
	ValidationRetries int
}

// TelemetryRuntimeConfig хранит параметры включения и инициализации telemetry backends.
type TelemetryRuntimeConfig struct {
	VerboseTracing         bool
	DebugArtifacts         bool
	DebugArtifactsMaxChars int
	LangfuseEnabled        bool
	Langfuse               telemetry.LangfuseOTLPConfig
}

// ResolvedRuntimeConfig содержит полностью подготовленные typed-конфиги для BuildRuntime.
type ResolvedRuntimeConfig struct {
	LoggerDebug bool

	StatePersistPath       string
	StateCacheBackplaneDir string
	SnapshotTimeout        time.Duration
	ToolRetryPolicy        retry.Policy

	LLM            llm.Config
	LLMModelPrices map[string]llm.ModelPrice

	Memory       MemoryRuntimeConfig
	ToolRegistry toolkit.RegistryConfig
	HTTPGetTool  toolkit.HTTPGetConfig
	MCP          MCPRuntimeConfig

	Planner    planner.Config
	Guardrails guardrails.Config
	Output     OutputRuntimeConfig
	Telemetry  TelemetryRuntimeConfig
	Runtime    core.RuntimeConfig

	ToolErrorDefaultMode core.ToolErrorMode
	ToolErrorFallback    map[string]core.ToolErrorMode
	MCPEnrichmentSources []core.MCPEnrichmentSource

	Skills         []string
	EnabledBundle  bool
	UserAuthHeader string
	AuthOAuth21    OAuth21ResourceServerConfig
	WebUIEnabled   bool
}

// ResolveRuntime загружает Config, применяет overrides и возвращает готовые параметры runtime wiring.
func ResolveRuntime(overrides RuntimeOverrides) (ResolvedRuntimeConfig, error) {
	cfg, err := Load()
	if err != nil {
		return ResolvedRuntimeConfig{}, err
	}
	cfg, err = applyRuntimeOverrides(cfg, overrides)
	if err != nil {
		return ResolvedRuntimeConfig{}, err
	}

	toolRetryPolicy := retry.Policy{
		MaxRetries:    cfg.Tools.MaxExecutionRetries,
		BaseDelay:     time.Duration(cfg.Tools.RetryBaseMs) * time.Millisecond,
		MaxDelay:      5 * time.Second,
		DisableJitter: cfg.LLM.DisableJitter,
	}

	modelPrices := make(map[string]llm.ModelPrice, len(cfg.Langfuse.ModelPrices))
	for model, price := range cfg.Langfuse.ModelPrices {
		modelPrices[model] = llm.ModelPrice{
			InputPer1M:  price.InputPer1M,
			OutputPer1M: price.OutputPer1M,
		}
	}

	toolErrorDefault, toolErrorFallback, err := resolveToolErrorPolicy(cfg.Agent)
	if err != nil {
		return ResolvedRuntimeConfig{}, err
	}

	return ResolvedRuntimeConfig{
		LoggerDebug: cfg.Logging.Debug,

		StatePersistPath:       strings.TrimSpace(cfg.State.PersistPath),
		StateCacheBackplaneDir: strings.TrimSpace(cfg.State.CacheBackplaneDir),
		SnapshotTimeout:        time.Duration(cfg.State.TimeoutMs) * time.Millisecond,
		ToolRetryPolicy:        toolRetryPolicy,

		LLM: llm.Config{
			Provider:                 cfg.LLM.Provider,
			Model:                    cfg.LLM.Model,
			BaseURL:                  cfg.LLM.BaseURL,
			OpenAIAPIKey:             cfg.LLM.OpenAIAPIKey,
			OpenRouterAPIKey:         cfg.LLM.OpenRouterAPIKey,
			OpenRouterHTTPReferer:    cfg.LLM.OpenRouterHTTPReferer,
			OpenRouterAppTitle:       cfg.LLM.OpenRouterAppTitle,
			TimeoutMs:                cfg.LLM.TimeoutMs,
			MaxRetries:               cfg.LLM.MaxRetries,
			RetryBaseMs:              cfg.LLM.RetryBaseMs,
			MaxParallel:              cfg.LLM.MaxParallel,
			CircuitBreakerFailures:   cfg.LLM.CircuitBreakerFailures,
			CircuitBreakerCooldownMs: cfg.LLM.CircuitBreakerCooldownMs,
			DisableJitter:            cfg.LLM.DisableJitter,
			CacheTTLms:               cfg.LLM.CacheTTLms,
		},
		LLMModelPrices: modelPrices,

		Memory: MemoryRuntimeConfig{
			ShortTermMaxMessages: cfg.Memory.ShortTermMaxMessages,
			RecallTopK:           cfg.Memory.RecallTopK,
			TokenBudget:          cfg.Memory.TokenBudget,
		},
		ToolRegistry: toolkit.RegistryConfig{
			Allowlist:           cloneStrings(cfg.Tools.Allowlist),
			Denylist:            cloneStrings(cfg.Tools.Denylist),
			DefaultTimeout:      time.Duration(cfg.Tools.DefaultTimeoutMs) * time.Millisecond,
			MaxOutputBytes:      cfg.Tools.MaxOutputBytes,
			MaxExecutionRetries: cfg.Tools.MaxExecutionRetries,
			RetryBase:           time.Duration(cfg.Tools.RetryBaseMs) * time.Millisecond,
			MaxParallel:         cfg.Tools.MaxParallel,
			DedupTTL:            time.Duration(cfg.Tools.DedupTTLms) * time.Millisecond,
		},
		HTTPGetTool: toolkit.HTTPGetConfig{
			AllowDomains: cloneStrings(cfg.Tools.HTTPAllowDomains),
			MaxBodyBytes: cfg.Tools.HTTPMaxBodyBytes,
			Timeout:      time.Duration(cfg.Tools.HTTPTimeoutMs) * time.Millisecond,
			CacheTTL:     time.Duration(cfg.Tools.HTTPReadCacheTTLms) * time.Millisecond,
		},
		MCP: MCPRuntimeConfig{
			Enabled: cfg.MCP.Enabled,
			Servers: cloneMCPServers(cfg.MCP.Servers),
		},

		Planner: planner.Config{
			MaxJSONRetries: cfg.Planner.ActionJSONRetries,
			Temperature:    cfg.LLM.Temperature,
			TopP:           cfg.LLM.TopP,
			Seed:           cfg.LLM.Seed,
			MaxTokens:      cfg.LLM.MaxOutputTokens,
		},
		Guardrails: guardrails.Config{
			MaxSteps:           cfg.Guardrails.MaxSteps,
			MaxToolCalls:       cfg.Guardrails.MaxToolCalls,
			MaxDuration:        time.Duration(cfg.Guardrails.MaxTimeMs) * time.Millisecond,
			MaxToolOutputBytes: cfg.Guardrails.MaxToolOutputBytes,
			ToolAllowlist:      cloneStrings(cfg.Tools.Allowlist),
		},
		Output: OutputRuntimeConfig{
			Policy: output.Policy{
				MaxChars:            cfg.Output.MaxChars,
				ForbiddenSubstrings: cloneStrings(cfg.Output.ForbiddenSubstrings),
			},
			Schema:            cfg.Output.JSONSchema,
			ValidationRetries: cfg.Output.ValidationRetries,
		},
		Telemetry: TelemetryRuntimeConfig{
			VerboseTracing:         cfg.Logging.VerboseTracing,
			DebugArtifacts:         cfg.Logging.DebugArtifacts,
			DebugArtifactsMaxChars: cfg.Logging.DebugArtifactsMaxChars,
			LangfuseEnabled:        cfg.Langfuse.Enabled,
			Langfuse: telemetry.LangfuseOTLPConfig{
				Host:             cfg.Langfuse.Host,
				PublicKey:        cfg.Langfuse.PublicKey,
				SecretKey:        cfg.Langfuse.SecretKey,
				ServiceName:      cfg.Langfuse.ServiceName,
				ServiceVersion:   cfg.Langfuse.ServiceVersion,
				Environment:      cfg.Langfuse.Environment,
				RequestTimeout:   time.Duration(cfg.Langfuse.TimeoutMs) * time.Millisecond,
				MaxArtifactChars: cfg.Logging.DebugArtifactsMaxChars,
			},
		},
		Runtime: core.RuntimeConfig{
			MaxStepTimeout:          time.Duration(cfg.Agent.MaxStepDurationMs) * time.Millisecond,
			MaxPlanningRetries:      cfg.Planner.MaxPlanningRetries,
			MaxInputChars:           cfg.Agent.MaxInputChars,
			OutputValidationRetries: cfg.Output.ValidationRetries,
			SnapshotTimeout:         time.Duration(cfg.State.TimeoutMs) * time.Millisecond,
			Deterministic:           cfg.Agent.Deterministic,
			EnabledSkills:           cloneStrings(cfg.Skills),
			RequireToolApproval:     cfg.Agent.RequireToolApproval,
			ToolApprovalAutoApprove: cloneStrings(cfg.Agent.ApprovalAutoApproveTools),
		},

		ToolErrorDefaultMode: toolErrorDefault,
		ToolErrorFallback:    toolErrorFallback,
		MCPEnrichmentSources: cloneMCPEnrichmentSources(cfg.Agent.MCPEnrichmentSources),

		Skills:         cloneStrings(cfg.Skills),
		EnabledBundle:  cfg.Bundle.Enabled,
		UserAuthHeader: cfg.Auth.UserAuthHeader,
		AuthOAuth21:    cloneOAuth21ResourceServerConfig(cfg.Auth.OAuth21),
		WebUIEnabled:   cfg.WebUI.Enabled,
	}, nil
}

// applyRuntimeOverrides применяет entrypoint overrides к Config и повторно валидирует результат.
func applyRuntimeOverrides(cfg Config, overrides RuntimeOverrides) (Config, error) {
	if provider := strings.TrimSpace(overrides.Provider); provider != "" {
		cfg.LLM.Provider = provider
	}
	if model := strings.TrimSpace(overrides.Model); model != "" {
		cfg.LLM.Model = model
	}
	if overrides.Debug != nil {
		cfg.Logging.Debug = *overrides.Debug
	}

	applyDerivedDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// resolveToolErrorPolicy преобразует строковые настройки tool_error_mode в типизированные значения core.
func resolveToolErrorPolicy(agentCfg AgentConfig) (core.ToolErrorMode, map[string]core.ToolErrorMode, error) {
	defaultMode, err := core.ParseToolErrorMode(agentCfg.ToolErrorMode)
	if err != nil {
		return "", nil, err
	}
	fallback := make(map[string]core.ToolErrorMode, len(agentCfg.ToolErrorFallback))
	for toolName, rawMode := range agentCfg.ToolErrorFallback {
		mode, err := core.ParseToolErrorMode(rawMode)
		if err != nil {
			return "", nil, fmt.Errorf("parse tool error fallback for %s: %w", toolName, err)
		}
		fallback[toolName] = mode
	}
	return defaultMode, fallback, nil
}

// cloneStrings возвращает копию string-слайса, чтобы исключить побочные мутации.
func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

// cloneMCPServers возвращает копию списка MCP-серверов.
func cloneMCPServers(values []MCPServerConfig) []MCPServerConfig {
	if len(values) == 0 {
		return nil
	}
	out := make([]MCPServerConfig, 0, len(values))
	for _, value := range values {
		value.OAuth21.Scopes = cloneStrings(value.OAuth21.Scopes)
		out = append(out, value)
	}
	return out
}

func cloneOAuth21ResourceServerConfig(value OAuth21ResourceServerConfig) OAuth21ResourceServerConfig {
	value.RequiredScopes = cloneStrings(value.RequiredScopes)
	value.AllowedAlgs = cloneStrings(value.AllowedAlgs)
	return value
}

// cloneMCPEnrichmentSources returns a copy of deterministic MCP enrichment sources.
func cloneMCPEnrichmentSources(values []MCPEnrichmentSource) []core.MCPEnrichmentSource {
	if len(values) == 0 {
		return nil
	}
	out := make([]core.MCPEnrichmentSource, 0, len(values))
	for _, value := range values {
		args := append([]byte(nil), value.Args...)
		if len(args) == 0 {
			args = []byte("{}")
		}
		out = append(out, core.MCPEnrichmentSource{
			Name:     strings.TrimSpace(value.Name),
			ToolName: strings.TrimSpace(value.ToolName),
			Args:     args,
			Required: value.Required,
		})
	}
	return out
}

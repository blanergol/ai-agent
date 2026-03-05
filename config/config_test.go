package config

import (
	"encoding/json"
	"testing"

	"github.com/blanergol/agent-core/core"
)

// TestLoadFromEnvOverridesValues проверяет, что переменные окружения корректно переопределяют дефолты.
func TestLoadFromEnvOverridesValues(t *testing.T) {
	t.Setenv("AGENT_CORE_LLM_PROVIDER", "ollama")
	t.Setenv("AGENT_CORE_LLM_MODEL", "llama3")
	t.Setenv("AGENT_CORE_TOOLS_ALLOWLIST", "time.now,http.get")
	t.Setenv("AGENT_CORE_TOOLS_MAX_EXECUTION_RETRIES", "3")
	t.Setenv("AGENT_CORE_TOOLS_RETRY_BASE_MS", "150")
	t.Setenv("AGENT_CORE_SKILLS", "ops")
	t.Setenv("AGENT_CORE_MCP_SERVERS", "name=local,base_url=http://localhost:8787,enabled=true")
	t.Setenv("AGENT_CORE_AGENT_CONTINUE_ON_TOOL_ERROR", "false")
	t.Setenv("AGENT_CORE_AGENT_TOOL_ERROR_MODE", "fail")
	t.Setenv("AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK", "http.get=continue")
	t.Setenv("AGENT_CORE_AGENT_MAX_INPUT_CHARS", "4096")
	t.Setenv("AGENT_CORE_AGENT_REQUIRE_TOOL_APPROVAL", "false")
	t.Setenv("AGENT_CORE_AGENT_APPROVAL_AUTO_APPROVE_TOOLS", "kv.put,mcp.obs.readonly")
	t.Setenv("AGENT_CORE_WEB_UI_ENABLED", "true")
	t.Setenv("MCP_TOKEN_LOCAL", "tok")

	// cfg должен содержать уже применённые env-значения.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.LLM.Provider != "ollama" {
		t.Fatalf("provider = %s", cfg.LLM.Provider)
	}
	if cfg.LLM.Model != "llama3" {
		t.Fatalf("model = %s", cfg.LLM.Model)
	}
	if len(cfg.Tools.Allowlist) != 2 {
		t.Fatalf("allowlist len = %d", len(cfg.Tools.Allowlist))
	}
	if cfg.Tools.MaxExecutionRetries != 3 {
		t.Fatalf("max execution retries = %d", cfg.Tools.MaxExecutionRetries)
	}
	if cfg.Tools.RetryBaseMs != 150 {
		t.Fatalf("tools retry base ms = %d", cfg.Tools.RetryBaseMs)
	}
	if len(cfg.MCP.Servers) != 1 {
		t.Fatalf("mcp servers len = %d", len(cfg.MCP.Servers))
	}
	if cfg.MCP.Servers[0].Token != "tok" {
		t.Fatalf("mcp token = %s", cfg.MCP.Servers[0].Token)
	}
	if cfg.Agent.ContinueOnToolError {
		t.Fatalf("continue_on_tool_error = %t", cfg.Agent.ContinueOnToolError)
	}
	if cfg.Agent.MaxInputChars != 4096 {
		t.Fatalf("max_input_chars = %d", cfg.Agent.MaxInputChars)
	}
	if cfg.Agent.ToolErrorMode != "fail" {
		t.Fatalf("tool_error_mode = %s", cfg.Agent.ToolErrorMode)
	}
	if cfg.Agent.ToolErrorFallback["http.get"] != "continue" {
		t.Fatalf("tool_error_fallback[http.get] = %s", cfg.Agent.ToolErrorFallback["http.get"])
	}
	if cfg.Agent.RequireToolApproval {
		t.Fatalf("require_tool_approval = %t, want false", cfg.Agent.RequireToolApproval)
	}
	if len(cfg.Agent.ApprovalAutoApproveTools) != 2 {
		t.Fatalf("approval_auto_approve_tools len = %d, want 2", len(cfg.Agent.ApprovalAutoApproveTools))
	}
	if !cfg.WebUI.Enabled {
		t.Fatalf("web_ui.enabled = %t, want true", cfg.WebUI.Enabled)
	}
}

// TestLoadRejectsInvalidInt проверяет отказ загрузки при невалидном числовом параметре.
func TestLoadRejectsInvalidInt(t *testing.T) {
	t.Setenv("AGENT_CORE_LLM_TIMEOUT_MS", "abc")
	if _, err := Load(); err == nil {
		t.Fatalf("expected parse error")
	}
}

// TestLoadRejectsInvalidWebUIBool проверяет отказ загрузки при невалидном bool-параметре web UI.
func TestLoadRejectsInvalidWebUIBool(t *testing.T) {
	t.Setenv("AGENT_CORE_WEB_UI_ENABLED", "maybe")
	if _, err := Load(); err == nil {
		t.Fatalf("expected parse error")
	}
}

// TestLoadTestModeEnablesDeterminism проверяет auto-настройки воспроизводимого test-режима.
func TestLoadTestModeEnablesDeterminism(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !cfg.Agent.Deterministic {
		t.Fatalf("agent deterministic = %t, want true", cfg.Agent.Deterministic)
	}
	if !cfg.LLM.DisableJitter {
		t.Fatalf("llm disable_jitter = %t, want true", cfg.LLM.DisableJitter)
	}
}

// TestLoadParsesDeterministicEnvOverrides проверяет явные env-переопределения deterministic-параметров.
func TestLoadParsesDeterministicEnvOverrides(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "dev")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("AGENT_CORE_AGENT_DETERMINISTIC", "true")
	t.Setenv("AGENT_CORE_LLM_DISABLE_JITTER", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !cfg.Agent.Deterministic {
		t.Fatalf("agent deterministic = %t, want true", cfg.Agent.Deterministic)
	}
	if !cfg.LLM.DisableJitter {
		t.Fatalf("llm disable_jitter = %t, want true", cfg.LLM.DisableJitter)
	}
}

func TestLoadParsesMCPEnrichmentSources(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv(
		"AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES",
		`[{"name":"kb","tool_name":"mcp.docs.search","args":{"query":"agent"},"required":true}]`,
	)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(cfg.Agent.MCPEnrichmentSources) != 1 {
		t.Fatalf("sources len = %d, want 1", len(cfg.Agent.MCPEnrichmentSources))
	}
	src := cfg.Agent.MCPEnrichmentSources[0]
	if src.Name != "kb" {
		t.Fatalf("name = %s, want kb", src.Name)
	}
	if src.ToolName != "mcp.docs.search" {
		t.Fatalf("tool_name = %s", src.ToolName)
	}
	if !src.Required {
		t.Fatalf("required = %t, want true", src.Required)
	}
	if !json.Valid(src.Args) {
		t.Fatalf("args are not valid json: %s", string(src.Args))
	}
}

func TestLoadParsesAuthOAuth21Config(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_ENABLED", "true")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_ISSUER_URL", "http://localhost:9000")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_AUDIENCE", "agent-core")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_REQUIRED_SCOPES", "agent.run,mcp.read")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_ALLOWED_ALGS", "RS256,ES256")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_CLOCK_SKEW_SEC", "120")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_ALLOW_INSECURE_HTTP", "true")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_SUBJECT_CLAIM", "sub")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_SCOPE_CLAIM", "scope")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !cfg.Auth.OAuth21.Enabled {
		t.Fatalf("auth.oauth2_1.enabled = %t, want true", cfg.Auth.OAuth21.Enabled)
	}
	if cfg.Auth.OAuth21.IssuerURL != "http://localhost:9000" {
		t.Fatalf("issuer_url = %s", cfg.Auth.OAuth21.IssuerURL)
	}
	if cfg.Auth.OAuth21.Audience != "agent-core" {
		t.Fatalf("audience = %s", cfg.Auth.OAuth21.Audience)
	}
	if len(cfg.Auth.OAuth21.RequiredScopes) != 2 {
		t.Fatalf("required_scopes len = %d, want 2", len(cfg.Auth.OAuth21.RequiredScopes))
	}
	if len(cfg.Auth.OAuth21.AllowedAlgs) != 2 {
		t.Fatalf("allowed_algs len = %d, want 2", len(cfg.Auth.OAuth21.AllowedAlgs))
	}
	if cfg.Auth.OAuth21.ClockSkewSec != 120 {
		t.Fatalf("clock_skew_sec = %d, want 120", cfg.Auth.OAuth21.ClockSkewSec)
	}
}

func TestLoadAuthOAuth21RequiresAudience(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_ENABLED", "true")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_ALLOW_INSECURE_HTTP", "true")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_ISSUER_URL", "http://localhost:9000")
	t.Setenv("AGENT_CORE_AUTH_OAUTH2_1_AUDIENCE", "")

	if _, err := Load(); err == nil {
		t.Fatalf("expected missing audience error")
	}
}

func TestLoadMCPServerOAuthSecretFallback(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv(
		"AGENT_CORE_MCP_SERVERS",
		`[{"name":"obs","base_url":"http://localhost:8787","enabled":true,"oauth2_1":{"enabled":true,"issuer_url":"http://localhost:9000","client_id":"agent-core","allow_insecure_http":true}}]`,
	)
	t.Setenv("MCP_OAUTH_CLIENT_SECRET_OBS", "oauth-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(cfg.MCP.Servers) != 1 {
		t.Fatalf("mcp servers len = %d", len(cfg.MCP.Servers))
	}
	server := cfg.MCP.Servers[0]
	if !server.OAuth21.Enabled {
		t.Fatalf("oauth2_1.enabled = %t, want true", server.OAuth21.Enabled)
	}
	if server.OAuth21.ClientSecret != "oauth-secret" {
		t.Fatalf("oauth2_1.client_secret = %s", server.OAuth21.ClientSecret)
	}
	if server.OAuth21.AuthMethod != "client_secret_basic" {
		t.Fatalf("oauth2_1.auth_method = %s, want client_secret_basic", server.OAuth21.AuthMethod)
	}
}

// TestLoadDerivesCacheBackplaneDirFromPersistPath проверяет автогенерацию cache_backplane_dir от persist_path.
func TestLoadDerivesCacheBackplaneDirFromPersistPath(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv("AGENT_CORE_STATE_PERSIST_PATH", "data/state.json")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.State.CacheBackplaneDir == "" {
		t.Fatalf("cache_backplane_dir is empty")
	}
}

// TestLoadOpenRouterDefaults проверяет derived defaults и secret fallback для OpenRouter.
func TestLoadOpenRouterDefaults(t *testing.T) {
	t.Setenv("AGENT_CORE_LLM_PROVIDER", "openrouter")
	t.Setenv("AGENT_CORE_LLM_BASE_URL", "")
	t.Setenv("OPENROUTER_API_KEY", "or-test-key")
	t.Setenv("OPENAI_API_KEY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.LLM.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("base_url = %s", cfg.LLM.BaseURL)
	}
	if cfg.LLM.OpenRouterAPIKey != "or-test-key" {
		t.Fatalf("openrouter_api_key = %s", cfg.LLM.OpenRouterAPIKey)
	}
}

// TestLoadOpenRouterRequiresToken проверяет валидацию обязательного ключа для OpenRouter.
func TestLoadOpenRouterRequiresToken(t *testing.T) {
	t.Setenv("AGENT_CORE_LLM_PROVIDER", "openrouter")
	t.Setenv("AGENT_CORE_LLM_OPENROUTER_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatalf("expected missing openrouter token error")
	}
}

// TestLoadLangfuseAutoEnableFromFallbacks verifies auto-enable when LANGFUSE_* fallbacks are provided.
func TestLoadLangfuseAutoEnableFromFallbacks(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv("AGENT_CORE_LANGFUSE_ENABLED", "false")
	t.Setenv("LANGFUSE_HOST", "http://langfuse-web:3000")
	t.Setenv("LANGFUSE_PUBLIC_KEY", "lf_pk_test")
	t.Setenv("LANGFUSE_SECRET_KEY", "lf_sk_test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !cfg.Langfuse.Enabled {
		t.Fatalf("langfuse enabled = %t, want true", cfg.Langfuse.Enabled)
	}
	if cfg.Langfuse.Host != "http://langfuse-web:3000" {
		t.Fatalf("langfuse host = %s", cfg.Langfuse.Host)
	}
}

// TestLoadLangfuseEnabledRequiresCredentials verifies validation for explicit langfuse enablement.
func TestLoadLangfuseEnabledRequiresCredentials(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv("AGENT_CORE_LANGFUSE_ENABLED", "true")
	t.Setenv("AGENT_CORE_LANGFUSE_HOST", "http://langfuse-web:3000")
	t.Setenv("AGENT_CORE_LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("AGENT_CORE_LANGFUSE_SECRET_KEY", "")
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")

	if _, err := Load(); err == nil {
		t.Fatalf("expected missing langfuse credentials error")
	}
}

func TestLoadParsesLangfuseModelPrices(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv(
		"AGENT_CORE_LANGFUSE_MODEL_PRICES",
		`{"openai/gpt-4o-mini":{"input_per_1m":0.15,"output_per_1m":0.6}}`,
	)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	price, ok := cfg.Langfuse.ModelPrices["openai/gpt-4o-mini"]
	if !ok {
		t.Fatalf("missing model price for openai/gpt-4o-mini")
	}
	if price.InputPer1M != 0.15 {
		t.Fatalf("input_per_1m = %v, want 0.15", price.InputPer1M)
	}
	if price.OutputPer1M != 0.6 {
		t.Fatalf("output_per_1m = %v, want 0.6", price.OutputPer1M)
	}
}

func TestLoadRejectsNegativeLangfuseModelPrices(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv(
		"AGENT_CORE_LANGFUSE_MODEL_PRICES",
		`{"openai/gpt-4o-mini":{"input_per_1m":-0.1,"output_per_1m":0.6}}`,
	)
	if _, err := Load(); err == nil {
		t.Fatalf("expected validation error for negative model pricing")
	}
}

// TestResolveRuntimeAppliesOverrides проверяет применение CLI overrides при подготовке runtime-конфига.
func TestResolveRuntimeAppliesOverrides(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	debug := true

	resolved, err := ResolveRuntime(RuntimeOverrides{
		Provider: "ollama",
		Model:    "llama3",
		Debug:    &debug,
	})
	if err != nil {
		t.Fatalf("resolve runtime error: %v", err)
	}
	if resolved.LLM.Provider != "ollama" {
		t.Fatalf("provider = %s, want ollama", resolved.LLM.Provider)
	}
	if resolved.LLM.Model != "llama3" {
		t.Fatalf("model = %s, want llama3", resolved.LLM.Model)
	}
	if !resolved.LoggerDebug {
		t.Fatalf("logger debug = %t, want true", resolved.LoggerDebug)
	}
	if resolved.ToolRegistry.DefaultTimeout <= 0 {
		t.Fatalf("tool default timeout = %s, want > 0", resolved.ToolRegistry.DefaultTimeout)
	}
	if !resolved.Runtime.RequireToolApproval {
		t.Fatalf("require_tool_approval = %t, want true", resolved.Runtime.RequireToolApproval)
	}
}

// TestResolveRuntimeBuildsTypedToolErrorPolicy проверяет преобразование строкового tool error режима в typed core mode.
func TestResolveRuntimeBuildsTypedToolErrorPolicy(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv("AGENT_CORE_AGENT_TOOL_ERROR_MODE", "fail")
	t.Setenv("AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK", "http.get=continue")

	resolved, err := ResolveRuntime(RuntimeOverrides{})
	if err != nil {
		t.Fatalf("resolve runtime error: %v", err)
	}
	if resolved.ToolErrorDefaultMode != core.ToolErrorModeFail {
		t.Fatalf("default tool error mode = %s, want %s", resolved.ToolErrorDefaultMode, core.ToolErrorModeFail)
	}
	if resolved.ToolErrorFallback["http.get"] != core.ToolErrorModeContinue {
		t.Fatalf(
			"tool_error_fallback[http.get] = %s, want %s",
			resolved.ToolErrorFallback["http.get"],
			core.ToolErrorModeContinue,
		)
	}
}

func TestResolveRuntimeMapsMCPEnrichmentSources(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv(
		"AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES",
		`[{"name":"kb","tool_name":"mcp.docs.search","args":{"query":"agent"},"required":true}]`,
	)
	resolved, err := ResolveRuntime(RuntimeOverrides{})
	if err != nil {
		t.Fatalf("resolve runtime error: %v", err)
	}
	if len(resolved.MCPEnrichmentSources) != 1 {
		t.Fatalf("sources len = %d, want 1", len(resolved.MCPEnrichmentSources))
	}
	if resolved.MCPEnrichmentSources[0].ToolName != "mcp.docs.search" {
		t.Fatalf("tool_name = %s", resolved.MCPEnrichmentSources[0].ToolName)
	}
	if !resolved.MCPEnrichmentSources[0].Required {
		t.Fatalf("required = %t, want true", resolved.MCPEnrichmentSources[0].Required)
	}
}

// TestResolveRuntimeRejectsInvalidProviderOverride проверяет валидацию неподдерживаемого provider override.
func TestResolveRuntimeRejectsInvalidProviderOverride(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	if _, err := ResolveRuntime(RuntimeOverrides{Provider: "invalid-provider"}); err == nil {
		t.Fatalf("expected invalid provider override error")
	}
}

func TestLoadParsesBundleEnabled(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv("AGENT_CORE_BUNDLE_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !cfg.Bundle.Enabled {
		t.Fatalf("bundle.enabled = %t, want true", cfg.Bundle.Enabled)
	}
}

func TestResolveRuntimeMapsBundlenabled(t *testing.T) {
	t.Setenv("AGENT_CORE_MODE", "test")
	t.Setenv("AGENT_CORE_BUNDLE_ENABLED", "true")

	resolved, err := ResolveRuntime(RuntimeOverrides{})
	if err != nil {
		t.Fatalf("resolve runtime error: %v", err)
	}
	if !resolved.EnabledBundle {
		t.Fatalf("bundle_enabled = %t, want true", resolved.EnabledBundle)
	}
}

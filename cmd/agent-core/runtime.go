package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/blanergol/agent-core/config"
	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/internal"
	"github.com/blanergol/agent-core/pkg/cache"
	"github.com/blanergol/agent-core/pkg/guardrails"
	"github.com/blanergol/agent-core/pkg/llm"
	"github.com/blanergol/agent-core/pkg/mcp"
	"github.com/blanergol/agent-core/pkg/memory"
	"github.com/blanergol/agent-core/pkg/oauth21"
	"github.com/blanergol/agent-core/pkg/output"
	"github.com/blanergol/agent-core/pkg/planner"
	skillspkg "github.com/blanergol/agent-core/pkg/skills"
	"github.com/blanergol/agent-core/pkg/stages"
	"github.com/blanergol/agent-core/pkg/state"
	"github.com/blanergol/agent-core/pkg/telemetry"
	toolkit "github.com/blanergol/agent-core/pkg/tools"
)

// Overrides описывает CLI/runtime переопределения, применяемые поверх базового Config.
type Overrides = config.RuntimeOverrides

// Runtime is a fully wired application runtime dependencies bundle.
type Runtime struct {
	Runner         core.AgentRuntime
	Logger         *slog.Logger
	UserAuthHeader string
	OAuthVerifier  oauth21.AccessTokenVerifier
	WebUIEnabled   bool
	Shutdown       func(context.Context) error
}

// BuildRuntime wires config, LLM, tools, memory, guardrails and agent runtime.
func BuildRuntime(ctx context.Context, overrides Overrides) (*Runtime, error) {
	resolved, err := config.ResolveRuntime(overrides)
	if err != nil {
		return nil, err
	}

	logger := buildLogger(resolved.LoggerDebug)

	store, err := state.NewKVStore(resolved.StatePersistPath)
	if err != nil {
		return nil, err
	}

	var sharedBackplane cache.Backplane
	if resolved.StateCacheBackplaneDir != "" {
		sharedBackplane = cache.NewFileBackplane(resolved.StateCacheBackplaneDir)
	}

	provider, err := llm.NewProvider(
		resolved.LLM,
		logger,
		llm.WithCacheBackplane(sharedBackplane),
		llm.WithModelPrices(resolved.LLMModelPrices),
	)
	if err != nil {
		return nil, err
	}

	mem := memory.NewManagerWithOptions(
		memory.NewShortTermMemory(resolved.Memory.ShortTermMaxMessages),
		memory.NewInMemoryLongTerm(),
		resolved.Memory.RecallTopK,
		resolved.Memory.TokenBudget,
	)

	toolRegistryCfg := resolved.ToolRegistry
	var oauthVerifier oauth21.AccessTokenVerifier
	if resolved.AuthOAuth21.Enabled {
		verifier, err := oauth21.NewVerifier(oauth21.VerifierConfig{
			IssuerURL:         resolved.AuthOAuth21.IssuerURL,
			JWKSURL:           resolved.AuthOAuth21.JWKSURL,
			Audience:          resolved.AuthOAuth21.Audience,
			RequiredScopes:    resolved.AuthOAuth21.RequiredScopes,
			AllowedAlgs:       resolved.AuthOAuth21.AllowedAlgs,
			ClockSkew:         time.Duration(resolved.AuthOAuth21.ClockSkewSec) * time.Second,
			SubjectClaim:      resolved.AuthOAuth21.SubjectClaim,
			ScopeClaim:        resolved.AuthOAuth21.ScopeClaim,
			AllowInsecureHTTP: resolved.AuthOAuth21.AllowInsecureHTTP,
			HTTPTimeout:       toolRegistryCfg.DefaultTimeout,
		})
		if err != nil {
			return nil, fmt.Errorf("init inbound oauth verifier: %w", err)
		}
		oauthVerifier = verifier
	}

	var bundle *internal.Bundle
	if resolved.EnabledBundle {
		bundle = internal.NewBundle()
		toolRegistryCfg.Allowlist = appendUniqueStrings(toolRegistryCfg.Allowlist, bundle.ToolNames()...)
	}
	toolRegistryCfg.CacheBackplane = sharedBackplane
	toolRegistry := toolkit.NewRegistry(toolRegistryCfg, logger)
	if err := toolRegistry.Register(toolkit.NewKVPutTool(store)); err != nil {
		return nil, err
	}
	if err := toolRegistry.Register(toolkit.NewKVGetTool(store)); err != nil {
		return nil, err
	}

	skillRegistry := skillspkg.NewRegistry()
	if err := skillRegistry.Register(skillspkg.NewOpsSkill(resolved.HTTPGetTool)); err != nil {
		return nil, err
	}
	if bundle != nil {
		if err := bundle.RegisterSkills(skillRegistry); err != nil {
			return nil, err
		}
	}
	skillsApplied, err := skillRegistry.Apply(resolved.Skills, toolRegistry)
	if err != nil {
		return nil, err
	}
	promptAdditions := skillsApplied.PromptAdditions
	pipelineMutations := skillsApplied.Pipeline
	if bundle != nil {
		if err := bundle.RegisterTools(toolRegistry); err != nil {
			return nil, err
		}
		promptAdditions = append(promptAdditions, bundle.PromptAdditions()...)
		pipelineMutations = append(pipelineMutations, bundle.PipelineMutations()...)
	}

	if resolved.MCP.Enabled {
		for _, server := range resolved.MCP.Servers {
			if !server.Enabled {
				continue
			}
			authenticator := mcp.NewStaticBearerAuthenticator(server.Token)
			if server.OAuth21.Enabled {
				tokenSource, err := oauth21.NewClientCredentialsTokenSource(oauth21.ClientCredentialsConfig{
					IssuerURL:         server.OAuth21.IssuerURL,
					TokenURL:          server.OAuth21.TokenURL,
					ClientID:          server.OAuth21.ClientID,
					ClientSecret:      server.OAuth21.ClientSecret,
					Audience:          server.OAuth21.Audience,
					Scopes:            server.OAuth21.Scopes,
					AuthMethod:        server.OAuth21.AuthMethod,
					ClockSkew:         time.Duration(server.OAuth21.ClockSkewSec) * time.Second,
					AllowInsecureHTTP: server.OAuth21.AllowInsecureHTTP,
					HTTPTimeout:       toolRegistryCfg.DefaultTimeout,
				})
				if err != nil {
					return nil, fmt.Errorf("init mcp oauth for %s: %w", server.Name, err)
				}
				authenticator = mcp.NewOAuthBearerAuthenticator(tokenSource)
			}
			bridge := mcp.Bridge{
				ServerName: server.Name,
				Client: mcp.NewHTTPClientWithAuthenticator(
					server.BaseURL,
					authenticator,
					toolRegistryCfg.DefaultTimeout,
					resolved.ToolRetryPolicy,
				),
			}
			if err := bridge.Import(ctx, toolRegistry); err != nil {
				return nil, fmt.Errorf("import mcp tools from %s: %w", server.Name, err)
			}
		}
	}

	pl := planner.NewDefaultPlanner(provider, resolved.Planner)

	gr := guardrails.New(resolved.Guardrails)

	policyValidator := output.NewPolicyValidator(resolved.Output.Policy)
	schemaValidator, err := output.NewSchemaValidator(resolved.Output.Schema)
	if err != nil {
		return nil, err
	}
	outputValidator := output.Compose(policyValidator, schemaValidator)

	telemetryTracers := make([]telemetry.Tracer, 0, 2)
	artifactSinks := make([]telemetry.ArtifactSink, 0, 2)
	shutdownFuncs := make([]func(context.Context) error, 0, 2)
	scoreSinks := make([]telemetry.ScoreSink, 0, 1)
	if resolved.Telemetry.VerboseTracing {
		telemetryTracers = append(telemetryTracers, telemetry.NewLoggerTracer(logger))
	}
	if resolved.Telemetry.DebugArtifacts {
		artifactSinks = append(artifactSinks, telemetry.NewLoggerArtifactSink(logger, resolved.Telemetry.DebugArtifactsMaxChars))
	}
	if resolved.Telemetry.LangfuseEnabled {
		langfuseBackend, err := telemetry.NewLangfuseBackend(resolved.Telemetry.Langfuse, logger)
		if err != nil {
			return nil, fmt.Errorf("init langfuse telemetry: %w", err)
		}
		telemetryTracers = append(telemetryTracers, langfuseBackend.Tracer)
		artifactSinks = append(artifactSinks, langfuseBackend.Artifacts)
		scoreSinks = append(scoreSinks, langfuseBackend.Scores)
		shutdownFuncs = append(shutdownFuncs, langfuseBackend.Shutdown)
	}
	tracer := telemetry.CombineTracers(telemetryTracers...)
	artifacts := telemetry.CombineArtifactSinks(artifactSinks...)
	scores := telemetry.CombineScoreSinks(scoreSinks...)
	shutdown := telemetry.JoinShutdownFuncs(shutdownFuncs...)
	metrics := telemetry.NoopMetrics{}

	toolErrorPolicy := core.NewStaticToolErrorPolicy(resolved.ToolErrorDefaultMode, resolved.ToolErrorFallback)
	interceptors := core.NewInterceptorRegistry()
	if len(resolved.MCPEnrichmentSources) > 0 {
		interceptors.RegisterInterceptor(
			core.PhaseEnrichContext,
			core.NewMCPContextEnrichment(resolved.MCPEnrichmentSources),
		)
	}
	if bundle != nil {
		bundle.RegisterInterceptors(interceptors)
	}
	toolExecutor := core.NewRegistryToolExecutor(toolRegistry, interceptors)

	pipeline, err := stages.BuildDefaultPipeline(stages.FactoryConfig{}, pipelineMutations...)
	if err != nil {
		return nil, err
	}

	runtimeCfg := resolved.Runtime
	runtimeCfg.PromptHints = promptAdditions

	runtime := core.NewRuntime(
		runtimeCfg,
		core.RuntimeDeps{
			Planner:         pl,
			Memory:          mem,
			State:           state.NewSessionSnapshotter(store),
			Tools:           toolRegistry,
			ToolExecutor:    toolExecutor,
			Interceptors:    interceptors,
			Guardrails:      gr,
			OutputValidator: outputValidator,
			ToolErrorPolicy: toolErrorPolicy,
			SnapshotStore:   state.NewSnapshotStoreWithPolicy(store, resolved.ToolRetryPolicy),
			ContextBinder: telemetry.NewContextBinder(
				tracer,
				metrics,
				artifacts,
				scores,
			),
		},
		pipeline,
	)

	return &Runtime{
		Runner:         runtime,
		Logger:         logger,
		UserAuthHeader: resolved.UserAuthHeader,
		OAuthVerifier:  oauthVerifier,
		WebUIEnabled:   resolved.WebUIEnabled,
		Shutdown:       shutdown,
	}, nil
}

func buildLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

func appendUniqueStrings(values []string, extras ...string) []string {
	if len(extras) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values)+len(extras))
	out := make([]string, 0, len(values)+len(extras))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range extras {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

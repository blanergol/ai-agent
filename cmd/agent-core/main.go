package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	appconfig "github.com/blanergol/agent-core/config"
	"github.com/blanergol/agent-core/internal/agent"
	"github.com/blanergol/agent-core/internal/cache"
	"github.com/blanergol/agent-core/internal/guardrails"
	"github.com/blanergol/agent-core/internal/llm"
	"github.com/blanergol/agent-core/internal/mcp"
	"github.com/blanergol/agent-core/internal/memory"
	"github.com/blanergol/agent-core/internal/output"
	"github.com/blanergol/agent-core/internal/planner"
	"github.com/blanergol/agent-core/internal/redact"
	"github.com/blanergol/agent-core/internal/retry"
	"github.com/blanergol/agent-core/internal/skills"
	"github.com/blanergol/agent-core/internal/state"
	"github.com/blanergol/agent-core/internal/telemetry"
	"github.com/blanergol/agent-core/internal/tools"
	"github.com/spf13/cobra"
)

// runtimeOverrides хранит CLI-переопределения runtime-параметров поверх базовой конфигурации.
type runtimeOverrides struct {
	// provider позволяет временно переопределить источник LLM через CLI.
	provider string
	// model задаёт альтернативную модель поверх конфигурации по умолчанию.
	model string
	// debug отличает "флаг не передан" от "явно выключить логирование".
	debug *bool
}

// runtimeDependencies объединяет ключевые зависимости, собранные на этапе buildRuntime.
type runtimeDependencies struct {
	// agent содержит собранный пайплайн планирования и выполнения шагов.
	agent *agent.Agent
	// logger используется для технического аудита выполнения агента.
	logger *slog.Logger
	// userAuthHeader задаёт HTTP-заголовок, из которого читается subject пользователя.
	userAuthHeader string
	// webUIEnabled включает встроенную web-страницу ручного тестирования.
	webUIEnabled bool
	// shutdown закрывает runtime-ресурсы (например, exporter telemetry).
	shutdown func(context.Context) error
}

// main запускает корневую CLI-команду и завершает процесс с кодом ошибки при сбое.
func main() {
	root := newRootCmd()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCmd собирает корневой CLI и подключает подкоманды запуска и HTTP-сервера.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agent-core",
		Short: "Base AI agent core",
	}
	root.AddCommand(newRunCmd())
	root.AddCommand(newServeCmd())
	return root
}

// newRunCmd создаёт CLI-команду для однократного выполнения агентной задачи.
func newRunCmd() *cobra.Command {
	var (
		// inputText хранит запрос пользователя из флага или stdin.
		inputText string
		// providerOverride задаёт провайдера LLM поверх конфигурации.
		providerOverride string
		// modelOverride задаёт модель LLM поверх конфигурации.
		modelOverride string
		// debugOverride включает подробное логирование для текущего запуска.
		debugOverride bool
		// userSub передаёт идентификатор субъекта для guardrails-контекста.
		userSub string
		// sessionID позволяет продолжить конкретную сессию диалога.
		sessionID string
		// correlationID позволяет явно задать идентификатор запроса.
		correlationID string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a single agent task",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serveSession := telemetry.EnsureSession(telemetry.SessionInfo{})
			serveCtx := telemetry.WithSession(cmd.Context(), serveSession)
			rt, err := buildRuntime(serveCtx, runtimeOverrides{
				provider: providerOverride,
				model:    modelOverride,
				debug:    optionalDebugOverride(cmd, debugOverride),
			})
			if err != nil {
				return err
			}
			defer shutdownRuntime(rt.logger, rt.shutdown)

			if strings.TrimSpace(inputText) == "" {
				// reader нужен, чтобы принять ввод из stdin в интерактивном режиме.
				reader := bufio.NewReader(os.Stdin)
				if _, err := fmt.Fprint(os.Stdout, "input> "); err != nil {
					return err
				}
				// line хранит необработанную строку пользователя до trim.
				line, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
					return err
				}
				inputText = strings.TrimSpace(line)
			}
			if inputText == "" {
				return fmt.Errorf("empty input")
			}

			result, err := rt.agent.RunWithInput(cmd.Context(), agent.RunInput{
				Text:          inputText,
				SessionID:     sessionID,
				CorrelationID: correlationID,
				Auth:          guardrails.UserAuthContext{Subject: userSub},
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(os.Stdout, result.FinalResponse); err != nil {
				return err
			}
			runLogCtx := telemetry.WithSession(cmd.Context(), telemetry.SessionInfo{
				SessionID:     result.SessionID,
				CorrelationID: result.CorrelationID,
			})
			telemetry.NewContextLogger(runLogCtx, rt.logger).Info("agent run finished",
				slog.Int("steps", result.Steps),
				slog.Int("tool_calls", result.ToolCalls),
				slog.String("stop_reason", result.StopReason),
				slog.String("api_version", result.APIVersion),
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&providerOverride, "provider", "", "LLM provider override: openai|openrouter|ollama|lmstudio")
	cmd.Flags().StringVar(&modelOverride, "model", "", "LLM model override")
	cmd.Flags().StringVar(&inputText, "input", "", "Task input (if empty, read from stdin)")
	cmd.Flags().BoolVar(&debugOverride, "debug", false, "Enable debug logging")
	cmd.Flags().StringVar(&userSub, "user-sub", "", "Optional user auth subject context")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Optional session id for context continuity")
	cmd.Flags().StringVar(&correlationID, "correlation-id", "", "Optional request correlation id")
	return cmd
}

// newServeCmd создаёт CLI-команду, поднимающую HTTP API для агента.
func newServeCmd() *cobra.Command {
	var (
		// addr определяет адрес и порт HTTP-сервера.
		addr string
		// firstOnly ограничивает сервер обработкой только первого успешного запроса.
		firstOnly bool
		// shutdownTimeoutMs задаёт окно мягкого завершения после первого запроса.
		shutdownTimeoutMs int
		// providerOverride задаёт провайдера LLM поверх конфигурации.
		providerOverride string
		// modelOverride задаёт модель LLM поверх конфигурации.
		modelOverride string
		// debugOverride включает подробный режим логирования.
		debugOverride bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start HTTP server for agent requests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serveSession := telemetry.EnsureSession(telemetry.SessionInfo{})
			serveCtx := telemetry.WithSession(cmd.Context(), serveSession)
			rt, err := buildRuntime(serveCtx, runtimeOverrides{
				provider: providerOverride,
				model:    modelOverride,
				debug:    optionalDebugOverride(cmd, debugOverride),
			})
			if err != nil {
				return err
			}
			defer shutdownRuntime(rt.logger, rt.shutdown)

			api := newAPIServer(rt.agent, rt.logger, rt.userAuthHeader, firstOnly, rt.webUIEnabled)
			// srv определяет сетевые тайм-ауты и маршрутизатор API.
			srv := &http.Server{
				Addr:              addr,
				Handler:           api.routes(),
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      2 * time.Minute,
				IdleTimeout:       60 * time.Second,
			}
			firstHandled := make(chan struct{})
			if firstOnly {
				var firstHandledOnce sync.Once
				api.onFirstHandled = func() {
					firstHandledOnce.Do(func() { close(firstHandled) })
				}
			} else {
				firstHandled = nil
			}
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			shutdownTimeout := time.Duration(shutdownTimeoutMs) * time.Millisecond
			return runHTTPServer(serveCtx, rt.logger, srv, listener, shutdownTimeout, firstHandled, firstOnly)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":8080", "HTTP listen address")
	cmd.Flags().BoolVar(&firstOnly, "first-only", true, "Process only the first successful request")
	cmd.Flags().IntVar(&shutdownTimeoutMs, "shutdown-timeout-ms", 5000, "Graceful shutdown timeout in milliseconds")
	cmd.Flags().StringVar(&providerOverride, "provider", "", "LLM provider override: openai|openrouter|ollama|lmstudio")
	cmd.Flags().StringVar(&modelOverride, "model", "", "LLM model override")
	cmd.Flags().BoolVar(&debugOverride, "debug", false, "Enable debug logging")
	return cmd
}

// buildRuntime связывает конфигурацию, LLM, память, инструменты и guardrails в единый runtime.
func buildRuntime(ctx context.Context, overrides runtimeOverrides) (*runtimeDependencies, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return nil, err
	}
	if overrides.provider != "" {
		cfg.LLM.Provider = overrides.provider
	}
	if overrides.model != "" {
		cfg.LLM.Model = overrides.model
	}
	if overrides.debug != nil {
		cfg.Logging.Debug = *overrides.debug
	}

	logger := buildLogger(cfg.Logging.Debug)
	retryPolicy := retry.Policy{
		MaxRetries:    cfg.Tools.MaxExecutionRetries,
		BaseDelay:     time.Duration(cfg.Tools.RetryBaseMs) * time.Millisecond,
		MaxDelay:      5 * time.Second,
		DisableJitter: cfg.LLM.DisableJitter,
	}

	// store хранит KV-состояние и при необходимости синхронизируется с файлом.
	store, err := state.NewKVStore(cfg.State.PersistPath)
	if err != nil {
		return nil, err
	}
	// sharedBackplane позволяет разделять кэш между несколькими инстансами runtime.
	var sharedBackplane cache.Backplane
	if strings.TrimSpace(cfg.State.CacheBackplaneDir) != "" {
		sharedBackplane = cache.NewFileBackplane(cfg.State.CacheBackplaneDir)
	}
	modelPrices := make(map[string]llm.ModelPrice, len(cfg.Langfuse.ModelPrices))
	for model, price := range cfg.Langfuse.ModelPrices {
		modelPrices[model] = llm.ModelPrice{
			InputPer1M:  price.InputPer1M,
			OutputPer1M: price.OutputPer1M,
		}
	}

	// provider инкапсулирует общение с выбранным LLM-провайдером.
	provider, err := llm.NewProvider(
		cfg.LLM,
		logger,
		llm.WithCacheBackplane(sharedBackplane),
		llm.WithModelPrices(modelPrices),
	)
	if err != nil {
		return nil, err
	}

	// mem объединяет короткую и долгую память для построения контекста.
	mem := memory.NewManagerWithOptions(
		memory.NewShortTermMemory(cfg.Memory.ShortTermMaxMessages),
		memory.NewInMemoryLongTerm(),
		cfg.Memory.RecallTopK,
		cfg.Memory.TokenBudget,
	)

	// toolRegistry применяет политики, тайм-ауты и аудит вызовов инструментов.
	toolRegistry := tools.NewRegistry(tools.RegistryConfig{
		Allowlist:           cfg.Tools.Allowlist,
		Denylist:            cfg.Tools.Denylist,
		DefaultTimeout:      time.Duration(cfg.Tools.DefaultTimeoutMs) * time.Millisecond,
		MaxOutputBytes:      cfg.Tools.MaxOutputBytes,
		MaxExecutionRetries: cfg.Tools.MaxExecutionRetries,
		RetryBase:           time.Duration(cfg.Tools.RetryBaseMs) * time.Millisecond,
		MaxParallel:         cfg.Tools.MaxParallel,
		DedupTTL:            time.Duration(cfg.Tools.DedupTTLms) * time.Millisecond,
		CacheBackplane:      sharedBackplane,
	}, logger)
	if err := toolRegistry.Register(tools.NewKVPutTool(store)); err != nil {
		return nil, err
	}
	if err := toolRegistry.Register(tools.NewKVGetTool(store)); err != nil {
		return nil, err
	}

	// skillRegistry подключает набор инструментов и подсказки промпта из включённых навыков.
	skillRegistry := skills.NewRegistry()
	if err := skillRegistry.Register(skills.NewOpsSkill(tools.HTTPGetConfig{
		AllowDomains: cfg.Tools.HTTPAllowDomains,
		MaxBodyBytes: cfg.Tools.HTTPMaxBodyBytes,
		Timeout:      time.Duration(cfg.Tools.HTTPTimeoutMs) * time.Millisecond,
		CacheTTL:     time.Duration(cfg.Tools.HTTPReadCacheTTLms) * time.Millisecond,
	})); err != nil {
		return nil, err
	}
	promptAdditions, err := skillRegistry.Apply(cfg.Skills, toolRegistry)
	if err != nil {
		return nil, err
	}

	if cfg.MCP.Enabled {
		for _, server := range cfg.MCP.Servers {
			if !server.Enabled {
				continue
			}
			// bridge импортирует удалённые MCP-инструменты в локальный реестр.
			bridge := mcp.Bridge{
				ServerName: server.Name,
				Client: mcp.NewHTTPClientWithPolicy(
					server.BaseURL,
					server.Token,
					time.Duration(cfg.Tools.DefaultTimeoutMs)*time.Millisecond,
					retryPolicy,
				),
			}
			if err := bridge.Import(ctx, toolRegistry); err != nil {
				return nil, fmt.Errorf("import mcp tools from %s: %w", server.Name, err)
			}
		}
	}

	// pl отвечает за вычисление следующего безопасного действия агента.
	pl := planner.NewDefaultPlanner(provider, planner.Config{
		MaxJSONRetries: cfg.Planner.ActionJSONRetries,
		Temperature:    cfg.LLM.Temperature,
		TopP:           cfg.LLM.TopP,
		Seed:           cfg.LLM.Seed,
		MaxTokens:      cfg.LLM.MaxOutputTokens,
	})

	// gr ограничивает число шагов, вызовов инструментов и объём вывода.
	gr := guardrails.New(guardrails.Config{
		MaxSteps:           cfg.Guardrails.MaxSteps,
		MaxToolCalls:       cfg.Guardrails.MaxToolCalls,
		MaxDuration:        time.Duration(cfg.Guardrails.MaxTimeMs) * time.Millisecond,
		MaxToolOutputBytes: cfg.Guardrails.MaxToolOutputBytes,
		ToolAllowlist:      cfg.Tools.Allowlist,
	})

	// outputValidator валидирует финальный ответ и блокирует небезопасный контент.
	policyValidator := output.NewPolicyValidator(output.Policy{
		MaxChars:            cfg.Output.MaxChars,
		ForbiddenSubstrings: cfg.Output.ForbiddenSubstrings,
	})
	schemaValidator, err := output.NewSchemaValidator(cfg.Output.JSONSchema)
	if err != nil {
		return nil, err
	}
	outputValidator := output.Compose(policyValidator, schemaValidator)
	// telemetryTracers собирает активные backends tracing.
	telemetryTracers := make([]telemetry.Tracer, 0, 2)
	// artifactSinks собирает активные sinks артефактов.
	artifactSinks := make([]telemetry.ArtifactSink, 0, 2)
	// shutdownFuncs аккумулирует cleanup callbacks runtime-компонентов.
	shutdownFuncs := make([]func(context.Context) error, 0, 2)
	// scoreSinks собирает backends для business/eval scores.
	scoreSinks := make([]telemetry.ScoreSink, 0, 1)
	if cfg.Logging.VerboseTracing {
		telemetryTracers = append(telemetryTracers, telemetry.NewLoggerTracer(logger))
	}
	if cfg.Logging.DebugArtifacts {
		artifactSinks = append(artifactSinks, telemetry.NewLoggerArtifactSink(logger, cfg.Logging.DebugArtifactsMaxChars))
	}
	if cfg.Langfuse.Enabled {
		langfuseBackend, err := telemetry.NewLangfuseBackend(telemetry.LangfuseOTLPConfig{
			Host:             cfg.Langfuse.Host,
			PublicKey:        cfg.Langfuse.PublicKey,
			SecretKey:        cfg.Langfuse.SecretKey,
			ServiceName:      cfg.Langfuse.ServiceName,
			ServiceVersion:   cfg.Langfuse.ServiceVersion,
			Environment:      cfg.Langfuse.Environment,
			RequestTimeout:   time.Duration(cfg.Langfuse.TimeoutMs) * time.Millisecond,
			MaxArtifactChars: cfg.Logging.DebugArtifactsMaxChars,
		}, logger)
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
	// metrics оставляет API-слой для подключения реального бэкенда метрик.
	metrics := telemetry.NoopMetrics{}
	defaultToolErrorMode, err := agent.ParseToolErrorMode(cfg.Agent.ToolErrorMode)
	if err != nil {
		return nil, err
	}
	toolErrorFallback := make(map[string]agent.ToolErrorMode, len(cfg.Agent.ToolErrorFallback))
	for toolName, rawMode := range cfg.Agent.ToolErrorFallback {
		mode, err := agent.ParseToolErrorMode(rawMode)
		if err != nil {
			return nil, fmt.Errorf("parse tool error fallback for %s: %w", toolName, err)
		}
		toolErrorFallback[toolName] = mode
	}
	toolErrorPolicy := agent.NewStaticToolErrorPolicy(defaultToolErrorMode, toolErrorFallback)

	// ag объединяет планировщик, память и инструменты в исполняемый цикл агента.
	ag := agent.NewWithConfig(
		pl,
		mem,
		store,
		toolRegistry,
		gr,
		logger,
		agent.RuntimeConfig{
			MaxStepTimeout:          time.Duration(cfg.Agent.MaxStepDurationMs) * time.Millisecond,
			MaxPlanningRetries:      cfg.Planner.MaxPlanningRetries,
			ContinueOnToolError:     cfg.Agent.ContinueOnToolError,
			ToolErrorPolicy:         toolErrorPolicy,
			MaxInputChars:           cfg.Agent.MaxInputChars,
			Deterministic:           cfg.Agent.Deterministic,
			OutputValidationRetries: cfg.Output.ValidationRetries,
			OutputValidator:         outputValidator,
			Tracer:                  tracer,
			Metrics:                 metrics,
			Artifacts:               artifacts,
			Scores:                  scores,
			SnapshotStore:           agent.NewKVSnapshotStoreWithPolicy(store, retryPolicy),
			SnapshotTimeout:         time.Duration(cfg.State.TimeoutMs) * time.Millisecond,
			EnabledSkills:           cfg.Skills,
		},
		promptAdditions,
	)

	return &runtimeDependencies{
		agent:          ag,
		logger:         logger,
		userAuthHeader: cfg.Auth.UserAuthHeader,
		webUIEnabled:   cfg.WebUI.Enabled,
		shutdown:       shutdown,
	}, nil
}

// optionalDebugOverride возвращает nil, если флаг debug не был явно передан пользователем.
func optionalDebugOverride(cmd *cobra.Command, value bool) *bool {
	if !cmd.Flags().Changed("debug") {
		return nil
	}
	// v копирует значение во временную переменную для корректного возврата указателя.
	v := value
	return &v
}

// buildLogger подбирает уровень логирования и создаёт JSON-обработчик для stdout.
func buildLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

// shutdownRuntime завершает фоновые runtime-компоненты с bounded timeout.
func shutdownRuntime(logger *slog.Logger, shutdown func(context.Context) error) {
	if shutdown == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		telemetry.NewContextLogger(ctx, logger).Warn(
			"runtime shutdown failed",
			slog.String("error", redact.Error(err)),
		)
	}
}

// runHTTPServer запускает HTTP-сервер и координирует его мягкое завершение по контексту или первому запросу.
func runHTTPServer(
	serveCtx context.Context,
	logger *slog.Logger,
	srv *http.Server,
	listener net.Listener,
	shutdownTimeout time.Duration,
	firstHandled <-chan struct{},
	firstOnly bool,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 5 * time.Second
	}

	var shutdownOnce sync.Once
	shutdownServer := func(reason string) {
		shutdownOnce.Do(func() {
			shutdownCtx, cancel := context.WithTimeout(serveCtx, shutdownTimeout)
			defer cancel()
			telemetry.NewContextLogger(shutdownCtx, logger).Info(
				"http server shutdown requested",
				slog.String("reason", reason),
			)
			if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				telemetry.NewContextLogger(shutdownCtx, logger).Error(
					"http shutdown failed",
					slog.String("error", redact.Error(err)),
				)
			}
		})
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-serveCtx.Done():
			shutdownServer("context_canceled")
		case <-firstHandled:
			shutdownServer("first_request_handled")
		case <-done:
		}
	}()

	addr := srv.Addr
	if listener != nil {
		addr = listener.Addr().String()
	}
	startAttrs := []slog.Attr{
		slog.String("addr", addr),
		slog.Bool("first_only", firstOnly),
		slog.String("endpoint", "/v1/agent/run"),
	}
	telemetry.NewContextLogger(serveCtx, logger).Info("http server started", startAttrs...)

	var err error
	if listener != nil {
		err = srv.Serve(listener)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	telemetry.NewContextLogger(serveCtx, logger).Info("http server stopped")
	return nil
}

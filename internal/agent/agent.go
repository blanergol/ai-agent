package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/blanergol/agent-core/internal/apperrors"
	"github.com/blanergol/agent-core/internal/guardrails"
	"github.com/blanergol/agent-core/internal/memory"
	"github.com/blanergol/agent-core/internal/output"
	"github.com/blanergol/agent-core/internal/planner"
	"github.com/blanergol/agent-core/internal/redact"
	"github.com/blanergol/agent-core/internal/state"
	"github.com/blanergol/agent-core/internal/telemetry"
	"github.com/blanergol/agent-core/internal/tools"
)

// Agent реализует основной цикл выполнения задачи: планирование, действие, рефлексия и завершение.
type Agent struct {
	planner planner.Planner
	memory  *memory.Manager
	state   state.Store
	tools   *tools.Registry

	guardrails *guardrails.Guardrails
	logger     *slog.Logger

	maxStepTimeout          time.Duration
	maxPlanningRetries      int
	toolErrorPolicy         ToolErrorPolicy
	maxInputChars           int
	outputValidationRetries int

	outputValidator  output.Validator
	promptHints      []string
	observer         Observer
	tracer           telemetry.Tracer
	metrics          telemetry.Metrics
	artifacts        telemetry.ArtifactSink
	scores           telemetry.ScoreSink
	snapshotStore    SnapshotStore
	snapshotTimeout  time.Duration
	deterministic    bool
	deterministicSeq uint64
}

// RuntimeConfig задаёт поведение и интеграции агента на этапе инициализации.
type RuntimeConfig struct {
	MaxStepTimeout          time.Duration
	MaxPlanningRetries      int
	ContinueOnToolError     bool
	ToolErrorPolicy         ToolErrorPolicy
	MaxInputChars           int
	OutputValidationRetries int
	OutputValidator         output.Validator
	Observer                Observer
	Tracer                  telemetry.Tracer
	Metrics                 telemetry.Metrics
	Artifacts               telemetry.ArtifactSink
	Scores                  telemetry.ScoreSink
	SnapshotStore           SnapshotStore
	SnapshotTimeout         time.Duration
	Deterministic           bool
}

// RunInput описывает строго типизированный контракт запуска агента.
type RunInput struct {
	// Text хранит пользовательскую задачу.
	Text string
	// SessionID связывает последовательность запросов одного диалога.
	SessionID string
	// CorrelationID связывает конкретный запрос внутри сессии.
	CorrelationID string
	// Auth передаёт аутентификационный контекст пользователя.
	Auth guardrails.UserAuthContext
}

// RunResult описывает итог выполнения одного запроса агента.
type RunResult struct {
	FinalResponse string
	Steps         int
	ToolCalls     int
	StopReason    string
	// SessionID возвращается клиенту для продолжения диалога.
	SessionID string
	// CorrelationID возвращается клиенту для трассировки запроса.
	CorrelationID string
	// APIVersion фиксирует версию публичного контракта результата.
	APIVersion string
}

// New создаёт Agent с совместимым legacy-контрактом и минимальной конфигурацией runtime.
func New(pl planner.Planner, mem *memory.Manager, st state.Store, tr *tools.Registry, gr *guardrails.Guardrails, logger *slog.Logger, maxStepTimeout time.Duration, promptHints []string) *Agent {
	return NewWithConfig(pl, mem, st, tr, gr, logger, RuntimeConfig{
		MaxStepTimeout: maxStepTimeout,
	}, promptHints)
}

// NewWithConfig создаёт Agent с расширенной конфигурацией ограничений, телеметрии и snapshot-подсистем.
func NewWithConfig(pl planner.Planner, mem *memory.Manager, st state.Store, tr *tools.Registry, gr *guardrails.Guardrails, logger *slog.Logger, cfg RuntimeConfig, promptHints []string) *Agent {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxStepTimeout <= 0 {
		cfg.MaxStepTimeout = 20 * time.Second
	}
	if cfg.MaxPlanningRetries < 0 {
		cfg.MaxPlanningRetries = 0
	}
	if cfg.MaxInputChars <= 0 {
		cfg.MaxInputChars = 8000
	}
	if cfg.Observer == nil {
		cfg.Observer = noopObserver{}
	}
	if cfg.OutputValidationRetries < 0 {
		cfg.OutputValidationRetries = 0
	}
	if cfg.OutputValidator == nil {
		cfg.OutputValidator = output.NewPolicyValidator(output.Policy{})
	}
	if cfg.ToolErrorPolicy == nil {
		defaultMode := ToolErrorModeFail
		if cfg.ContinueOnToolError {
			defaultMode = ToolErrorModeContinue
		}
		cfg.ToolErrorPolicy = NewStaticToolErrorPolicy(defaultMode, nil)
	}
	if cfg.Tracer == nil {
		cfg.Tracer = telemetry.NoopTracer{}
	}
	if cfg.Metrics == nil {
		cfg.Metrics = telemetry.NoopMetrics{}
	}
	if cfg.Artifacts == nil {
		cfg.Artifacts = telemetry.NoopArtifactSink{}
	}
	if cfg.Scores == nil {
		cfg.Scores = telemetry.NoopScoreSink{}
	}
	if cfg.SnapshotStore == nil {
		cfg.SnapshotStore = NoopSnapshotStore{}
	}
	if cfg.SnapshotTimeout <= 0 {
		cfg.SnapshotTimeout = 1500 * time.Millisecond
	}
	if mem == nil {
		mem = memory.NewManager(nil, nil, 5)
	}
	if gr == nil {
		gr = guardrails.New(guardrails.Config{})
	}

	return &Agent{
		planner:                 pl,
		memory:                  mem,
		state:                   st,
		tools:                   tr,
		guardrails:              gr,
		logger:                  logger,
		maxStepTimeout:          cfg.MaxStepTimeout,
		maxPlanningRetries:      cfg.MaxPlanningRetries,
		toolErrorPolicy:         cfg.ToolErrorPolicy,
		maxInputChars:           cfg.MaxInputChars,
		outputValidationRetries: cfg.OutputValidationRetries,
		outputValidator:         cfg.OutputValidator,
		promptHints:             promptHints,
		observer:                cfg.Observer,
		tracer:                  cfg.Tracer,
		metrics:                 cfg.Metrics,
		artifacts:               cfg.Artifacts,
		scores:                  cfg.Scores,
		snapshotStore:           cfg.SnapshotStore,
		snapshotTimeout:         cfg.SnapshotTimeout,
		deterministic:           cfg.Deterministic,
	}
}

// Run сохраняет обратную совместимость старого контракта запуска.
func (a *Agent) Run(ctx context.Context, userInput string, auth guardrails.UserAuthContext) (RunResult, error) {
	return a.RunWithInput(ctx, RunInput{
		Text: userInput,
		Auth: auth,
	})
}

// RunWithInput запускает полный цикл observe -> plan -> act -> update -> finalize.
func (a *Agent) RunWithInput(ctx context.Context, in RunInput) (runResult RunResult, runErr error) {
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" {
		return RunResult{}, apperrors.New(apperrors.CodeBadRequest, "empty input", false)
	}
	if len(in.Text) > a.maxInputChars {
		return RunResult{}, apperrors.New(apperrors.CodeValidation, fmt.Sprintf("input exceeds max chars: %d", a.maxInputChars), false)
	}
	if a.deterministic {
		seq := atomic.AddUint64(&a.deterministicSeq, 1)
		if strings.TrimSpace(in.SessionID) == "" {
			in.SessionID = fmt.Sprintf("session-%06d", seq)
		}
		if strings.TrimSpace(in.CorrelationID) == "" {
			in.CorrelationID = fmt.Sprintf("corr-%06d", seq)
		}
	}

	session := telemetry.EnsureSession(telemetry.SessionInfo{
		SessionID:     in.SessionID,
		CorrelationID: in.CorrelationID,
		UserSub:       in.Auth.Subject,
	})
	ctx = telemetry.WithSession(ctx, session)
	ctx = telemetry.BindRuntimeWithArtifacts(ctx, a.tracer, a.metrics, a.artifacts)
	ctx = telemetry.WithScores(ctx, a.scores)
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "agent.run", map[string]any{"input_hash": shortHash(in.Text)})
	var spanErr error
	defer func() { span.End(spanErr) }()
	defer func() { a.emitRunScores(ctx, runResult, runErr) }()

	// runMemory и runGuardrails создаются на каждый запуск, чтобы состояние не текло между запросами.
	runMemory := a.memory.NewRun()
	runGuardrails := a.guardrails.NewRun()
	loadCtx, loadCancel := context.WithTimeout(ctx, a.snapshotTimeout)
	if snapshot, ok, err := a.snapshotStore.Load(loadCtx, session.SessionID); err == nil && ok {
		if snapshot.APIVersion == "" || snapshot.APIVersion == APIVersion {
			runMemory.RestoreShortTerm(snapshot.ShortTermMessages)
			runGuardrails.Restore(snapshot.Guardrails)
		}
	} else if err != nil {
		telemetry.NewContextLogger(ctx, a.logger).Warn(
			"snapshot load failed",
			slog.String("error", redact.Error(err)),
		)
	}
	loadCancel()
	defer a.persistSnapshot(ctx, session.SessionID, runMemory, runGuardrails)

	if err := runMemory.AddUserMessage(ctx, in.Text); err != nil {
		a.notify(ctx, Event{Type: EventRunFailed, Error: redact.Error(err)})
		spanErr = err
		return RunResult{}, err
	}
	a.notify(ctx, Event{Type: EventRunStarted, InputHash: shortHash(in.Text)})
	telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
		Kind:    telemetry.ArtifactPrompt,
		Name:    "agent.user_input",
		Payload: in.Text,
	})

	outputValidationAttempts := 0
	actionRepeats := make(map[string]int)

	for {
		if err := runGuardrails.BeforeStep(); err != nil {
			steps, toolCalls, _ := runGuardrails.Stats()
			result := RunResult{
				FinalResponse: fallbackFinalResponse(redact.Error(err)),
				Steps:         steps,
				ToolCalls:     toolCalls,
				StopReason:    redact.Error(err),
				SessionID:     session.SessionID,
				CorrelationID: session.CorrelationID,
				APIVersion:    APIVersion,
			}
			telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
				Kind:    telemetry.ArtifactState,
				Name:    "agent.run_result",
				Payload: runResultArtifactPayload(result),
			})
			telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
				Kind:    telemetry.ArtifactResponse,
				Name:    "agent.final_response",
				Payload: result.FinalResponse,
			})
			a.notify(ctx, Event{Type: EventRunCompleted, Step: steps, StopReason: result.StopReason})
			return result, nil
		}
		step, _, _ := runGuardrails.Stats()

		stepCtx, cancel := context.WithTimeout(ctx, a.maxStepTimeout)
		next, err := a.planWithRetries(stepCtx, in.Text, in.Auth, runMemory)
		if err != nil {
			cancel()
			a.notify(ctx, Event{Type: EventRunFailed, Step: step, Error: redact.Error(err)})
			spanErr = err
			return RunResult{}, err
		}
		fingerprint := actionFingerprint(next.Action)
		actionRepeats[fingerprint]++
		if actionRepeats[fingerprint] > 3 {
			cancel()
			steps, toolCalls, _ := runGuardrails.Stats()
			result := RunResult{
				FinalResponse: fallbackFinalResponse("repeated_action_detected"),
				Steps:         steps,
				ToolCalls:     toolCalls,
				StopReason:    "repeated_action_detected",
				SessionID:     session.SessionID,
				CorrelationID: session.CorrelationID,
				APIVersion:    APIVersion,
			}
			telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
				Kind:    telemetry.ArtifactState,
				Name:    "agent.run_result",
				Payload: runResultArtifactPayload(result),
			})
			telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
				Kind:    telemetry.ArtifactResponse,
				Name:    "agent.final_response",
				Payload: result.FinalResponse,
			})
			a.notify(ctx, Event{Type: EventRunCompleted, Step: steps, StopReason: result.StopReason})
			return result, nil
		}

		a.notify(stepCtx, Event{
			Type:       EventStepPlanned,
			Step:       step,
			ActionType: next.Action.Type,
			ToolName:   next.Action.ToolName,
		})

		if err := runGuardrails.ValidateAction(next.Action); err != nil {
			cancel()
			a.notify(stepCtx, Event{Type: EventRunFailed, Step: step, Error: redact.Error(err)})
			spanErr = err
			return RunResult{}, err
		}

		result, done, err := a.act(stepCtx, step, next, runMemory, runGuardrails)
		cancel()
		if err != nil {
			a.notify(ctx, Event{Type: EventRunFailed, Step: step, Error: redact.Error(err)})
			spanErr = err
			return RunResult{}, err
		}

		if stop, reason := a.reflect(next, result, done); stop {
			finalResponse := strings.TrimSpace(result)
			if finalResponse == "" {
				finalResponse = fallbackFinalResponse(reason)
			}
			if err := a.outputValidator.Validate(ctx, finalResponse); err != nil {
				if outputValidationAttempts < a.outputValidationRetries {
					outputValidationAttempts++
					_ = runMemory.AddSystemMessage(
						ctx,
						"Final response was rejected by output policy. Return a corrected safe response only.",
					)
					a.notify(ctx, Event{
						Type:       EventOutputInvalid,
						Step:       step,
						StopReason: redact.Error(err),
						OutputHash: shortHash(result),
					})
					continue
				}
				a.notify(ctx, Event{Type: EventRunFailed, Step: step, Error: redact.Error(err)})
				spanErr = err
				return RunResult{}, err
			}

			steps, toolCalls, _ := runGuardrails.Stats()
			runResult := RunResult{
				FinalResponse: finalResponse,
				Steps:         steps,
				ToolCalls:     toolCalls,
				StopReason:    reason,
				SessionID:     session.SessionID,
				CorrelationID: session.CorrelationID,
				APIVersion:    APIVersion,
			}
			telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
				Kind:    telemetry.ArtifactResponse,
				Name:    "agent.final_response",
				Payload: finalResponse,
			})
			a.notify(ctx, Event{
				Type:       EventRunCompleted,
				Step:       steps,
				StopReason: reason,
				OutputHash: shortHash(finalResponse),
			})
			a.metrics.IncCounter("agent.run", 1, map[string]string{"status": "ok"})
			return runResult, nil
		}
	}
}

// planWithRetries повторяет шаг планирования при временных сбоях до исчерпания лимита retry.
func (a *Agent) planWithRetries(ctx context.Context, userInput string, auth guardrails.UserAuthContext, mem *memory.Manager) (planner.NextAction, error) {
	var lastErr error
	for attempt := 0; attempt <= a.maxPlanningRetries; attempt++ {
		next, err := a.plan(ctx, userInput, auth, mem)
		if err == nil {
			return next, nil
		}
		lastErr = err
		telemetry.NewContextLogger(ctx, a.logger).Warn(
			"planner attempt failed",
			slog.Int("attempt", attempt+1),
			slog.Int("max_retries", a.maxPlanningRetries),
			slog.String("error", redact.Error(err)),
		)
		if ctx.Err() != nil {
			break
		}
	}
	return planner.NextAction{}, apperrors.Wrap(apperrors.CodeTransient, "planner failed after retries", lastErr, true)
}

// plan строит наблюдение для планировщика и запрашивает у него следующее действие.
func (a *Agent) plan(ctx context.Context, userInput string, auth guardrails.UserAuthContext, mem *memory.Manager) (planner.NextAction, error) {
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "agent.plan", nil)
	var spanErr error
	defer func() { span.End(spanErr) }()

	ctxMessages, err := mem.BuildContext(ctx, userInput)
	if err != nil {
		spanErr = err
		return planner.NextAction{}, err
	}

	snippets := make([]string, 0, len(ctxMessages)+len(a.promptHints)+1)
	for _, msg := range ctxMessages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		prefix := string(msg.Role)
		if msg.Name != "" {
			prefix += "[" + msg.Name + "]"
		}
		snippets = append(snippets, prefix+": "+content)
	}
	snippets = append(snippets, a.promptHints...)
	if auth.Subject != "" {
		snippets = append(snippets, "Authenticated subject: "+auth.Subject)
	}
	telemetry.ArtifactsFromContext(ctx).Save(ctx, telemetry.Artifact{
		Kind:    telemetry.ArtifactPrompt,
		Name:    "agent.plan_observation",
		Payload: strings.Join(snippets, "\n"),
	})

	specs := a.tools.Specs()
	toolSpecs := make([]planner.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		toolSpecs = append(toolSpecs, planner.ToolSpec{
			Name:         spec.Name,
			Description:  spec.Description,
			InputSchema:  spec.InputSchema,
			OutputSchema: spec.OutputSchema,
		})
	}

	var snapshot map[string]any
	if a.state != nil {
		snapshot = state.SnapshotForSession(ctx, a.state.Snapshot())
	}
	return a.planner.Plan(ctx, planner.Observation{
		UserInput:      userInput,
		StateSnapshot:  snapshot,
		MemorySnippets: snippets,
		ToolCatalog:    toolSpecs,
	})
}

// act исполняет выбранное действие планировщика и обновляет память/guardrails по результату.
func (a *Agent) act(ctx context.Context, step int, next planner.NextAction, mem *memory.Manager, gr *guardrails.Guardrails) (result string, done bool, err error) {
	ctx, span := telemetry.TracerFromContext(ctx).Start(ctx, "agent.act", map[string]any{"action": next.Action.Type})
	var spanErr error
	defer func() { span.End(spanErr) }()

	switch next.Action.Type {
	case "final":
		if err := mem.AddAssistantMessage(ctx, next.Action.FinalResponse); err != nil {
			spanErr = err
			return "", false, err
		}
		return next.Action.FinalResponse, true, nil
	case "noop":
		if next.Done {
			final := strings.TrimSpace(next.Action.FinalResponse)
			if final == "" {
				final = strings.TrimSpace(next.Action.ExpectedOutcome)
			}
			if final == "" {
				final = "No further action is required."
			}
			if err := mem.AddAssistantMessage(ctx, final); err != nil {
				spanErr = err
				return "", false, err
			}
			return final, true, nil
		}
		return "", false, nil
	case "tool":
		toolResult, err := a.tools.Execute(ctx, next.Action.ToolName, next.Action.ToolArgs)
		if err != nil {
			toolErrText := "error: " + redact.Error(err)
			if grErr := gr.RecordToolCall(len(toolErrText)); grErr != nil {
				spanErr = grErr
				return "", false, grErr
			}
			if memErr := mem.AddToolResult(ctx, next.Action.ToolName, toolErrText); memErr != nil {
				spanErr = memErr
				return "", false, memErr
			}

			telemetry.NewContextLogger(ctx, a.logger).Warn(
				"tool action failed",
				slog.String("tool", next.Action.ToolName),
				slog.String("error", redact.Error(err)),
			)
			a.notify(ctx, Event{
				Type:       EventToolFailed,
				Step:       step,
				ActionType: next.Action.Type,
				ToolName:   next.Action.ToolName,
				Error:      redact.Error(err),
				OutputHash: shortHash(toolErrText),
			})

			decision := a.toolErrorPolicy.Decide(ctx, next.Action.ToolName, err)
			if decision.Continue() {
				return toolErrText, false, nil
			}
			spanErr = err
			return "", false, err
		}
		if err := gr.RecordToolCall(len(toolResult.Output)); err != nil {
			spanErr = err
			return "", false, err
		}
		if err := mem.AddToolResult(ctx, next.Action.ToolName, toolResult.Output); err != nil {
			spanErr = err
			return "", false, err
		}
		telemetry.NewContextLogger(ctx, a.logger).Debug(
			"tool action complete",
			slog.String("tool", next.Action.ToolName),
			slog.String("out_hash", shortHash(toolResult.Output)),
		)
		a.notify(ctx, Event{
			Type:       EventToolCompleted,
			Step:       step,
			ActionType: next.Action.Type,
			ToolName:   next.Action.ToolName,
			OutputHash: shortHash(toolResult.Output),
		})

		if next.Done {
			if err := mem.AddAssistantMessage(ctx, toolResult.Output); err != nil {
				spanErr = err
				return "", false, err
			}
			return toolResult.Output, true, nil
		}
		return toolResult.Output, false, nil
	default:
		unsupported := apperrors.New(apperrors.CodeValidation, fmt.Sprintf("unsupported action type: %s", next.Action.Type), false)
		spanErr = unsupported
		return "", false, unsupported
	}
}

// reflect определяет, нужно ли завершать цикл после выполненного действия.
func (a *Agent) reflect(next planner.NextAction, result string, done bool) (bool, string) {
	if done {
		return true, "planner_done"
	}
	if next.Done {
		return true, "stop_condition"
	}
	if next.Action.Type == "tool" && result == "" {
		return false, "continue"
	}
	return false, "continue"
}

// notify нормализует и безопасно отправляет событие наблюдателю, изолируя возможные panic.
func (a *Agent) notify(ctx context.Context, event Event) {
	if a.observer == nil {
		return
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	session := telemetry.SessionFromContext(ctx)
	if strings.TrimSpace(event.SessionID) == "" {
		event.SessionID = session.SessionID
	}
	if strings.TrimSpace(event.CorrelationID) == "" {
		event.CorrelationID = session.CorrelationID
	}
	if strings.TrimSpace(event.UserSub) == "" {
		event.UserSub = session.UserSub
	}
	if strings.TrimSpace(event.UserSub) != "" {
		// Для observer передаём только псевдоним субъекта, чтобы исключить утечку raw идентификатора.
		event.UserSub = telemetry.UserSubForLogs(event.UserSub)
	}
	// Приводим потенциально чувствительные поля к безопасному виду перед отправкой в observer.
	event.Error = redact.Text(event.Error)
	event.StopReason = redact.Text(event.StopReason)
	defer func() {
		if rec := recover(); rec != nil {
			telemetry.NewContextLogger(ctx, a.logger).Error(
				"agent observer panic recovered",
				slog.String("panic", redact.Text(fmt.Sprint(rec))),
			)
		}
	}()
	a.observer.OnEvent(ctx, event)
}

// shortHash возвращает короткий стабильный хэш строки для логов и телеметрии без утечки исходного текста.
func shortHash(text string) string {
	b, _ := json.Marshal(text)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func runResultArtifactPayload(result RunResult) string {
	payload := map[string]any{
		"final_response": result.FinalResponse,
		"stop_reason":    result.StopReason,
		"steps":          result.Steps,
		"tool_calls":     result.ToolCalls,
		"api_version":    result.APIVersion,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"stop_reason":%q}`, result.StopReason)
	}
	return string(raw)
}

// fallbackFinalResponse генерирует безопасный текст, если цикл остановился без финального ответа модели.
func fallbackFinalResponse(stopReason string) string {
	reason := strings.TrimSpace(stopReason)
	if reason == "" {
		reason = "unknown_stop_reason"
	}
	return fmt.Sprintf("Agent stopped before producing a final response (%s).", reason)
}

// actionFingerprint строит компактный hash для детекта повторяющихся действий планировщика.
func actionFingerprint(action planner.Action) string {
	// payload нормализует эквивалентные действия (разные reasoning_summary не должны ломать детект цикла).
	payload := map[string]any{
		"type": action.Type,
	}
	switch action.Type {
	case "tool":
		payload["tool_name"] = strings.TrimSpace(action.ToolName)
		var args any = map[string]any{}
		if len(action.ToolArgs) > 0 {
			if err := json.Unmarshal(action.ToolArgs, &args); err != nil {
				// fallback к сырому JSON, если аргументы невалидны.
				args = strings.TrimSpace(string(action.ToolArgs))
			}
		}
		payload["tool_args"] = args
	case "final":
		payload["final_response"] = strings.TrimSpace(action.FinalResponse)
	case "noop":
		// Для noop достаточно типа — ожидаем, что done контролируется отдельно контрактом planner.
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

func (a *Agent) emitRunScores(ctx context.Context, result RunResult, runErr error) {
	sink := telemetry.ScoresFromContext(ctx)
	success := 0.0
	comment := strings.TrimSpace(result.StopReason)
	if runErr != nil {
		comment = redact.Error(runErr)
	} else if strings.TrimSpace(result.FinalResponse) != "" {
		success = 1
	}
	sink.Save(ctx, telemetry.Score{
		Name:    "agent.run.success",
		Value:   success,
		Comment: comment,
	})
	sink.Save(ctx, telemetry.Score{
		Name:  "agent.run.steps",
		Value: float64(result.Steps),
	})
	sink.Save(ctx, telemetry.Score{
		Name:  "agent.run.tool_calls",
		Value: float64(result.ToolCalls),
	})
}

// persistSnapshot сохраняет минимальное состояние выполнения для последующего продолжения сессии.
func (a *Agent) persistSnapshot(ctx context.Context, sessionID string, mem *memory.Manager, gr *guardrails.Guardrails) {
	if a.snapshotStore == nil || strings.TrimSpace(sessionID) == "" || mem == nil || gr == nil {
		return
	}
	saveCtx, cancel := context.WithTimeout(ctx, a.snapshotTimeout)
	defer cancel()
	snapshot := RuntimeSnapshot{
		APIVersion:        APIVersion,
		SessionID:         sessionID,
		ShortTermMessages: mem.ShortTermSnapshot(),
		Guardrails:        gr.Snapshot(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := a.snapshotStore.Save(saveCtx, snapshot); err != nil {
		telemetry.NewContextLogger(saveCtx, a.logger).Warn(
			"snapshot save failed",
			slog.String("error", redact.Error(err)),
		)
	}
}

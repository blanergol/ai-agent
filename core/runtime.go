package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/blanergol/agent-core/pkg/redact"
)

// AgentRuntime is the public runtime contract used by transport layers.
type AgentRuntime interface {
	Run(ctx context.Context, in RunInput) (RunResult, error)
}

// RuntimeConfig controls runtime behavior.
type RuntimeConfig struct {
	MaxStepTimeout          time.Duration
	MaxPlanningRetries      int
	MaxInputChars           int
	OutputValidationRetries int
	SnapshotTimeout         time.Duration
	Deterministic           bool
	PromptHints             []string
	EnabledSkills           []string
}

// RuntimeDeps are runtime dependencies exposed as interfaces.
type RuntimeDeps struct {
	Planner         Planner
	Memory          Memory
	State           StateSnapshotter
	Tools           ToolRegistry
	ToolExecutor    ToolExecutor
	Interceptors    *InterceptorRegistry
	Guardrails      Guardrails
	OutputValidator OutputValidator
	ToolErrorPolicy ToolErrorPolicy
	SnapshotStore   SnapshotStore
	Observer        Observer
	ContextBinder   ContextBinder
}

// Runtime orchestrates run lifecycle and pipeline execution.
type Runtime struct {
	cfg      RuntimeConfig
	deps     RuntimeDeps
	pipeline *Pipeline

	deterministicSeq uint64
}

// NewRuntime creates a runtime around configurable pipeline and dependencies.
func NewRuntime(cfg RuntimeConfig, deps RuntimeDeps, pipeline *Pipeline) *Runtime {
	if cfg.MaxStepTimeout <= 0 {
		cfg.MaxStepTimeout = 20 * time.Second
	}
	if cfg.MaxInputChars <= 0 {
		cfg.MaxInputChars = 8000
	}
	if cfg.MaxPlanningRetries < 0 {
		cfg.MaxPlanningRetries = 0
	}
	if cfg.OutputValidationRetries < 0 {
		cfg.OutputValidationRetries = 0
	}
	if cfg.SnapshotTimeout <= 0 {
		cfg.SnapshotTimeout = 1500 * time.Millisecond
	}
	if deps.OutputValidator == nil {
		deps.OutputValidator = noopOutputValidator{}
	}
	if deps.Interceptors == nil {
		deps.Interceptors = NewInterceptorRegistry()
	}
	if deps.ToolExecutor == nil && deps.Tools != nil {
		deps.ToolExecutor = NewRegistryToolExecutor(deps.Tools, deps.Interceptors)
	}
	if deps.ToolErrorPolicy == nil {
		deps.ToolErrorPolicy = NewStaticToolErrorPolicy(ToolErrorModeFail, nil)
	}
	if deps.SnapshotStore == nil {
		deps.SnapshotStore = noopSnapshotStore{}
	}
	if deps.Observer == nil {
		deps.Observer = noopObserver{}
	}
	if pipeline == nil {
		pipeline = NewPipeline()
	}
	cfg.EnabledSkills = normalizeUniqueStrings(cfg.EnabledSkills)
	return &Runtime{
		cfg:      cfg,
		deps:     deps,
		pipeline: pipeline,
	}
}

// Run executes one agent task.
func (r *Runtime) Run(ctx context.Context, in RunInput) (RunResult, error) {
	rawInput := in.Text
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" {
		return RunResult{}, apperrors.New(apperrors.CodeBadRequest, "empty input", false)
	}
	if len(in.Text) > r.cfg.MaxInputChars {
		return RunResult{}, apperrors.New(
			apperrors.CodeValidation,
			fmt.Sprintf("input exceeds max chars: %d", r.cfg.MaxInputChars),
			false,
		)
	}
	if r.cfg.Deterministic {
		seq := atomic.AddUint64(&r.deterministicSeq, 1)
		if strings.TrimSpace(in.SessionID) == "" {
			in.SessionID = fmt.Sprintf("session-%06d", seq)
		}
		if strings.TrimSpace(in.CorrelationID) == "" {
			in.CorrelationID = fmt.Sprintf("corr-%06d", seq)
		}
	}

	meta := RunMeta{
		SessionID:     strings.TrimSpace(in.SessionID),
		CorrelationID: strings.TrimSpace(in.CorrelationID),
		UserSub:       strings.TrimSpace(in.UserSub),
	}
	if r.deps.ContextBinder != nil {
		meta = r.deps.ContextBinder.Ensure(meta)
		ctx = r.deps.ContextBinder.Bind(ctx, meta)
	}
	in.SessionID = meta.SessionID
	in.CorrelationID = meta.CorrelationID

	if r.deps.Planner == nil || r.deps.Tools == nil || r.deps.ToolExecutor == nil || r.deps.Memory == nil || r.deps.Guardrails == nil {
		return RunResult{}, apperrors.New(apperrors.CodeInternal, "runtime dependencies are not initialized", false)
	}

	runMemory := r.deps.Memory.NewRun()
	runGuardrails := r.deps.Guardrails.NewRun()
	if runMemory == nil || runGuardrails == nil {
		return RunResult{}, apperrors.New(apperrors.CodeInternal, "runtime dependencies returned nil run scope", false)
	}

	run := &RunContext{
		Input:         in,
		Meta:          meta,
		Config:        r.cfg,
		Deps:          r.deps,
		Memory:        runMemory,
		Guardrails:    runGuardrails,
		State:         NewAgentState(rawInput),
		ActionRepeats: make(map[string]int),
	}
	run.State.NormalizedInput = in.Text
	run.State.Budgets.TimeLimit = r.cfg.MaxStepTimeout

	r.restoreSnapshot(ctx, run)
	defer r.persistSnapshot(ctx, run)

	if err := run.ExecutePhase(ctx, PhaseInput, nil); err != nil {
		run.Notify(ctx, Event{Type: EventRunFailed, Error: redact.Error(err)})
		return RunResult{}, err
	}
	if normalized := strings.TrimSpace(run.State.NormalizedInput); normalized != "" {
		run.Input.Text = normalized
	}
	if err := run.Memory.AddUserMessage(ctx, run.Input.Text); err != nil {
		run.Notify(ctx, Event{Type: EventRunFailed, Error: redact.Error(err)})
		return RunResult{}, err
	}
	run.Notify(ctx, Event{Type: EventRunStarted, InputHash: shortHash(run.Input.Text)})

	for {
		stepCtx, cancel := context.WithTimeout(ctx, r.cfg.MaxStepTimeout)
		stageResult, err := r.pipeline.RunCycle(stepCtx, run)
		cancel()
		if err != nil {
			run.Notify(ctx, Event{Type: EventRunFailed, Step: run.CurrentStep, Error: redact.Error(err)})
			return RunResult{}, err
		}
		run.RecordIterationMetric(ctx, stageResult.Control)
		switch stageResult.Control {
		case StageControlRetry:
			if stageResult.Backoff > 0 {
				timer := time.NewTimer(stageResult.Backoff)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					return RunResult{}, ctx.Err()
				}
			}
			continue
		case StageControlStop:
			result := run.BuildResult()
			if strings.TrimSpace(result.StopReason) == "" {
				result.StopReason = strings.TrimSpace(stageResult.Reason)
			}
			if strings.TrimSpace(result.StopReason) == "" {
				result.StopReason = "stop_condition"
			}
			if strings.TrimSpace(result.FinalResponse) == "" {
				result.FinalResponse = strings.TrimSpace(run.PendingFinalResponse)
			}
			if strings.TrimSpace(result.FinalResponse) == "" {
				result.FinalResponse = FallbackFinalResponse(result.StopReason)
			}
			run.Notify(ctx, Event{
				Type:       EventRunCompleted,
				Step:       result.Steps,
				StopReason: result.StopReason,
				OutputHash: shortHash(result.FinalResponse),
			})
			return result, nil
		default:
			continue
		}
	}
}

func (r *Runtime) restoreSnapshot(ctx context.Context, run *RunContext) {
	if r.deps.SnapshotStore == nil || strings.TrimSpace(run.Meta.SessionID) == "" {
		return
	}
	loadCtx, cancel := context.WithTimeout(ctx, r.cfg.SnapshotTimeout)
	defer cancel()
	snapshot, ok, err := r.deps.SnapshotStore.Load(loadCtx, run.Meta.SessionID)
	if err != nil || !ok {
		return
	}
	if snapshot.APIVersion != "" && snapshot.APIVersion != APIVersion {
		return
	}
	run.Memory.RestoreShortTerm(snapshot.ShortTermMessages)
	// Guardrails лимиты применяются к одному Run-вызову и не должны
	// переноситься между HTTP-запросами одной сессии.
}

func (r *Runtime) persistSnapshot(ctx context.Context, run *RunContext) {
	if r.deps.SnapshotStore == nil || strings.TrimSpace(run.Meta.SessionID) == "" {
		return
	}
	saveCtx, cancel := context.WithTimeout(ctx, r.cfg.SnapshotTimeout)
	defer cancel()
	_ = r.deps.SnapshotStore.Save(saveCtx, RuntimeSnapshot{
		APIVersion:        APIVersion,
		SessionID:         run.Meta.SessionID,
		ShortTermMessages: run.Memory.ShortTermSnapshot(),
		Guardrails:        GuardrailsSnapshot{},
		UpdatedAt:         time.Now().UTC(),
	})
}

func shortHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func normalizeUniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		candidate := strings.TrimSpace(value)
		if candidate == "" {
			continue
		}
		seen := false
		for _, existing := range out {
			if existing == candidate {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, candidate)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

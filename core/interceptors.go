package core

import (
	"context"
	"sync"
	"time"
)

// PhaseInterceptor runs deterministic logic before/after a phase.
type PhaseInterceptor interface {
	Name() string
	BeforePhase(ctx context.Context, run *RunContext, phase Phase) error
	AfterPhase(ctx context.Context, run *RunContext, phase Phase, phaseErr error) error
}

// ToolExecutionFunc is the terminal or wrapped tool execution function.
type ToolExecutionFunc func(ctx context.Context, run *RunContext, call ToolCall) (ToolResult, error)

// ToolExecutionInterceptor wraps tool execution and can block/rewrite calls.
type ToolExecutionInterceptor interface {
	Name() string
	AroundToolExecution(ctx context.Context, run *RunContext, call ToolCall, next ToolExecutionFunc) (ToolResult, error)
}

// InterceptorRegistry keeps ordered interceptors for phases and tool execution.
type InterceptorRegistry struct {
	mu sync.RWMutex

	phase map[Phase][]PhaseInterceptor
	tool  []ToolExecutionInterceptor
}

// NewInterceptorRegistry creates an empty interceptor registry.
func NewInterceptorRegistry() *InterceptorRegistry {
	return &InterceptorRegistry{
		phase: map[Phase][]PhaseInterceptor{},
	}
}

// RegisterInterceptor appends ordered phase interceptor for one phase.
func (r *InterceptorRegistry) RegisterInterceptor(phase Phase, interceptor PhaseInterceptor) {
	if r == nil || interceptor == nil || phase == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase[phase] = append(r.phase[phase], interceptor)
}

// RegisterToolInterceptor appends ordered tool-execution interceptor.
func (r *InterceptorRegistry) RegisterToolInterceptor(interceptor ToolExecutionInterceptor) {
	if r == nil || interceptor == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tool = append(r.tool, interceptor)
}

func (r *InterceptorRegistry) beforePhase(ctx context.Context, run *RunContext, phase Phase) error {
	for _, interceptor := range r.phaseInterceptors(phase) {
		if err := interceptor.BeforePhase(ctx, run, phase); err != nil {
			return err
		}
	}
	return nil
}

func (r *InterceptorRegistry) afterPhase(ctx context.Context, run *RunContext, phase Phase, phaseErr error) error {
	interceptors := r.phaseInterceptors(phase)
	for i := len(interceptors) - 1; i >= 0; i-- {
		if err := interceptors[i].AfterPhase(ctx, run, phase, phaseErr); err != nil {
			return err
		}
	}
	return nil
}

func (r *InterceptorRegistry) phaseInterceptors(phase Phase) []PhaseInterceptor {
	if r == nil || phase == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	src := r.phase[phase]
	if len(src) == 0 {
		return nil
	}
	out := make([]PhaseInterceptor, len(src))
	copy(out, src)
	return out
}

// ToolInterceptors returns a safe copy of registered tool interceptors.
func (r *InterceptorRegistry) ToolInterceptors() []ToolExecutionInterceptor {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.tool) == 0 {
		return nil
	}
	out := make([]ToolExecutionInterceptor, len(r.tool))
	copy(out, r.tool)
	return out
}

// ExecutePhase runs phase interceptors around provided phase body and records trace.
func (r *RunContext) ExecutePhase(ctx context.Context, phase Phase, runFn func(context.Context) error) error {
	if r == nil {
		return nil
	}
	r.ensureState()
	startedAt := time.Now().UTC()
	r.Notify(ctx, Event{
		Type:      EventPhaseStarted,
		Step:      r.CurrentStep,
		Iteration: r.State.Iteration,
		Phase:     phase,
	})

	var phaseErr error
	if r.Deps.Interceptors != nil {
		if err := r.Deps.Interceptors.beforePhase(ctx, r, phase); err != nil {
			phaseErr = err
		}
	}
	if phaseErr == nil && runFn != nil {
		phaseErr = runFn(ctx)
	}
	if r.Deps.Interceptors != nil {
		if afterErr := r.Deps.Interceptors.afterPhase(ctx, r, phase, phaseErr); afterErr != nil && phaseErr == nil {
			phaseErr = afterErr
		}
	}

	r.recordPhaseTrace(ctx, phase, startedAt, phaseErr)
	if phaseErr != nil {
		r.State.appendErrorText(phaseErr.Error())
	}
	return phaseErr
}

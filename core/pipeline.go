package core

import (
	"context"
	"time"
)

// Stage is a pipeline primitive.
type Stage interface {
	Name() string
	Run(ctx context.Context, run *RunContext) (StageResult, error)
}

// StageControl defines what pipeline should do after a stage.
type StageControl string

const (
	StageControlContinue StageControl = "continue"
	StageControlRetry    StageControl = "retry"
	StageControlStop     StageControl = "stop"
)

// StageResult is stage execution outcome.
type StageResult struct {
	Control StageControl
	Reason  string
	Backoff time.Duration
}

func Continue() StageResult {
	return StageResult{Control: StageControlContinue}
}

func Retry(reason string) StageResult {
	return StageResult{Control: StageControlRetry, Reason: reason}
}

func Stop(reason string) StageResult {
	return StageResult{Control: StageControlStop, Reason: reason}
}

// StageMiddleware wraps stage execution.
type StageMiddleware func(Stage) Stage

// PipelineHooks are lifecycle callbacks around stage execution.
type PipelineHooks struct {
	BeforeStage func(ctx context.Context, run *RunContext, stage Stage)
	AfterStage  func(ctx context.Context, run *RunContext, stage Stage, result StageResult)
	OnError     func(ctx context.Context, run *RunContext, stage Stage, err error)
	OnStop      func(ctx context.Context, run *RunContext, stage Stage, result StageResult)
}

// Pipeline is a mutable ordered collection of stages.
type Pipeline struct {
	stages      []Stage
	middlewares []StageMiddleware
	hooks       PipelineHooks
}

func NewPipeline(stages ...Stage) *Pipeline {
	out := make([]Stage, 0, len(stages))
	for _, stage := range stages {
		if stage == nil {
			continue
		}
		out = append(out, stage)
	}
	return &Pipeline{stages: out}
}

func (p *Pipeline) StageNames() []string {
	out := make([]string, 0, len(p.stages))
	for _, stage := range p.stages {
		out = append(out, stage.Name())
	}
	return out
}

func (p *Pipeline) SetHooks(hooks PipelineHooks) {
	p.hooks = hooks
}

func (p *Pipeline) Use(middleware StageMiddleware) {
	if middleware == nil {
		return
	}
	p.middlewares = append(p.middlewares, middleware)
}

func (p *Pipeline) Append(stage Stage) {
	if stage == nil {
		return
	}
	p.stages = append(p.stages, stage)
}

func (p *Pipeline) InsertBefore(target string, stage Stage) bool {
	if stage == nil {
		return false
	}
	for i, existing := range p.stages {
		if existing.Name() != target {
			continue
		}
		p.stages = append(p.stages[:i], append([]Stage{stage}, p.stages[i:]...)...)
		return true
	}
	return false
}

func (p *Pipeline) InsertAfter(target string, stage Stage) bool {
	if stage == nil {
		return false
	}
	for i, existing := range p.stages {
		if existing.Name() != target {
			continue
		}
		next := i + 1
		p.stages = append(p.stages[:next], append([]Stage{stage}, p.stages[next:]...)...)
		return true
	}
	return false
}

func (p *Pipeline) Replace(target string, replacement Stage) bool {
	if replacement == nil {
		return false
	}
	for i, existing := range p.stages {
		if existing.Name() != target {
			continue
		}
		p.stages[i] = replacement
		return true
	}
	return false
}

func (p *Pipeline) Remove(target string) bool {
	for i, existing := range p.stages {
		if existing.Name() != target {
			continue
		}
		p.stages = append(p.stages[:i], p.stages[i+1:]...)
		return true
	}
	return false
}

// RunCycle executes one full stage cycle and returns first non-continue outcome.
func (p *Pipeline) RunCycle(ctx context.Context, run *RunContext) (StageResult, error) {
	for _, stage := range p.stages {
		wrapped := p.wrap(stage)
		if p.hooks.BeforeStage != nil {
			p.hooks.BeforeStage(ctx, run, wrapped)
		}
		result, err := wrapped.Run(ctx, run)
		if err != nil {
			if p.hooks.OnError != nil {
				p.hooks.OnError(ctx, run, wrapped, err)
			}
			return StageResult{}, err
		}
		if result.Control == "" {
			result.Control = StageControlContinue
		}
		if p.hooks.AfterStage != nil {
			p.hooks.AfterStage(ctx, run, wrapped, result)
		}
		if result.Control == StageControlStop {
			if p.hooks.OnStop != nil {
				p.hooks.OnStop(ctx, run, wrapped, result)
			}
			return result, nil
		}
		if result.Control == StageControlRetry {
			return result, nil
		}
	}
	return Continue(), nil
}

func (p *Pipeline) wrap(stage Stage) Stage {
	wrapped := stage
	for i := len(p.middlewares) - 1; i >= 0; i-- {
		wrapped = p.middlewares[i](wrapped)
	}
	return wrapped
}

package core

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func (r *RunContext) ensureState() {
	if r == nil {
		return
	}
	if r.State == nil {
		r.State = NewAgentState(r.Input.Text)
	}
	if r.State.Context == nil {
		r.State.Context = map[string]any{}
	}
	if r.State.Trace.RunStartedAt.IsZero() {
		r.State.Trace.RunStartedAt = time.Now().UTC()
	}
	if r.State.Trace.Debug == nil {
		r.State.Trace.Debug = map[string]any{}
	}
}

func (r *RunContext) recordPhaseTrace(ctx context.Context, phase Phase, startedAt time.Time, phaseErr error) {
	if r == nil {
		return
	}
	r.ensureState()
	endedAt := time.Now().UTC()
	trace := PhaseTrace{
		Iteration: r.State.Iteration,
		Phase:     phase,
		StartedAt: startedAt,
		EndedAt:   endedAt,
		Duration:  endedAt.Sub(startedAt),
	}
	if phaseErr != nil {
		trace.Error = strings.TrimSpace(phaseErr.Error())
	}
	r.State.Trace.Phases = append(r.State.Trace.Phases, trace)
	r.State.Budgets.TimeUsed = endedAt.Sub(r.State.Trace.RunStartedAt)
	r.Notify(
		ctx,
		Event{
			Type:       EventPhaseCompleted,
			Step:       r.CurrentStep,
			Iteration:  r.State.Iteration,
			Phase:      phase,
			DurationMs: trace.Duration.Milliseconds(),
			Error:      trace.Error,
		},
	)
}

// RecordToolExecution appends tool traces to state observability fields.
func (r *RunContext) RecordToolExecution(ctx context.Context, call ToolCall, result ToolResult, err error, duration time.Duration) {
	if r == nil {
		return
	}
	r.ensureState()
	args := append(json.RawMessage(nil), call.Args...)
	callTrace := ToolCallTrace{
		Iteration: call.Iteration,
		ToolName:  strings.TrimSpace(call.Name),
		ToolArgs:  args,
		Source:    strings.TrimSpace(call.Source),
		Duration:  duration,
		Timestamp: time.Now().UTC(),
	}
	if callTrace.Iteration <= 0 {
		callTrace.Iteration = r.State.Iteration
	}
	if err != nil {
		callTrace.Error = strings.TrimSpace(err.Error())
		r.State.appendErrorText(callTrace.Error)
	}
	r.State.ToolCallsHistory = append(r.State.ToolCallsHistory, callTrace)
	if err == nil {
		r.State.ToolResults = append(r.State.ToolResults, ToolResultTrace{
			Iteration: callTrace.Iteration,
			ToolName:  callTrace.ToolName,
			Output:    result.Output,
			Metadata:  copyMap(result.Metadata),
			Timestamp: callTrace.Timestamp,
		})
	}
	r.Notify(
		ctx,
		Event{
			Type:       EventToolTraced,
			Step:       r.CurrentStep,
			Iteration:  callTrace.Iteration,
			ActionType: ActionTypeTool,
			ToolName:   callTrace.ToolName,
			DurationMs: duration.Milliseconds(),
			Error:      callTrace.Error,
		},
	)
}

// RecordIterationMetric captures runtime-loop metrics for observability.
func (r *RunContext) RecordIterationMetric(ctx context.Context, control StageControl) {
	if r == nil {
		return
	}
	r.ensureState()
	steps, toolCalls, elapsed := 0, 0, time.Duration(0)
	if r.Guardrails != nil {
		steps, toolCalls, elapsed = r.Guardrails.Stats()
	}
	if r.State.Iteration <= 0 {
		r.State.Iteration = steps
	}
	r.State.Guardrails.Steps = steps
	r.State.Guardrails.ToolCalls = toolCalls
	r.State.Guardrails.Elapsed = elapsed
	r.State.Budgets.TimeUsed = elapsed

	metric := IterationMetric{
		Iteration: r.State.Iteration,
		Steps:     steps,
		ToolCalls: toolCalls,
		Elapsed:   elapsed,
		Control:   control,
		Timestamp: time.Now().UTC(),
	}
	r.State.Trace.Iterations = append(r.State.Trace.Iterations, metric)
	r.Notify(
		ctx,
		Event{
			Type:       EventIterationMetric,
			Step:       r.CurrentStep,
			Iteration:  metric.Iteration,
			DurationMs: metric.Elapsed.Milliseconds(),
		},
	)
}

package stages

import (
	"context"
	"fmt"
	"strings"

	"github.com/blanergol/agent-core/core"
	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/blanergol/agent-core/pkg/redact"
)

// ActStage выполняет выбранное действие и фиксирует результат в памяти run-контекста.
type ActStage struct{}

// NewActStage создает стадию выполнения действия, выбранного планировщиком.
func NewActStage() core.Stage {
	return &ActStage{}
}

// Name возвращает стабильный идентификатор стадии выполнения.
func (s *ActStage) Name() string { return "act" }

// Run исполняет final/noop/tool действие и обновляет состояние текущего запуска.
func (s *ActStage) Run(ctx context.Context, run *core.RunContext) (core.StageResult, error) {
	if run.PendingStop {
		return core.Continue(), nil
	}
	if err := run.ExecutePhase(ctx, core.PhaseGuardrails, func(_ context.Context) error {
		return run.Guardrails.ValidateAction(run.NextAction.Action)
	}); err != nil {
		return core.StageResult{}, err
	}

	action := run.NextAction.Action
	run.ActionResult = ""
	run.ActionDone = false
	run.ToolInvoked = ""

	switch action.Type {
	case core.ActionTypeFinal:
		if err := run.Memory.AddAssistantMessage(ctx, action.FinalResponse); err != nil {
			return core.StageResult{}, err
		}
		run.ActionResult = action.FinalResponse
		run.ActionDone = true
		return core.Continue(), nil
	case core.ActionTypeNoop:
		if !run.NextAction.Done {
			return core.Continue(), nil
		}
		final := strings.TrimSpace(action.FinalResponse)
		if final == "" {
			final = strings.TrimSpace(action.ExpectedOutcome)
		}
		if final == "" {
			final = "No further action is required."
		}
		if err := run.Memory.AddAssistantMessage(ctx, final); err != nil {
			return core.StageResult{}, err
		}
		run.ActionResult = final
		run.ActionDone = true
		return core.Continue(), nil
	case core.ActionTypeTool:
		if err := run.ExecutePhase(ctx, core.PhaseBeforeToolExecution, nil); err != nil {
			return core.StageResult{}, err
		}
		action = run.NextAction.Action
		run.ToolInvoked = strings.TrimSpace(action.ToolName)
		var toolResult core.ToolResult
		err := run.ExecutePhase(ctx, core.PhaseToolExecution, func(ctx context.Context) error {
			var execErr error
			toolResult, execErr = run.Deps.ToolExecutor.Execute(ctx, run, core.ToolCall{
				Name:      action.ToolName,
				Args:      action.ToolArgs,
				Source:    "planner",
				Iteration: run.CurrentStep,
			})
			return execErr
		})
		if err != nil {
			if afterErr := run.ExecutePhase(ctx, core.PhaseAfterToolExecution, nil); afterErr != nil {
				return core.StageResult{}, afterErr
			}
			toolErrText := "error: " + redact.Error(err)
			if grErr := run.Guardrails.RecordToolCall(len(toolErrText)); grErr != nil {
				return core.StageResult{}, grErr
			}
			if memErr := run.Memory.AddToolResult(ctx, action.ToolName, toolErrText); memErr != nil {
				return core.StageResult{}, memErr
			}
			run.Notify(ctx, core.Event{
				Type:       core.EventToolFailed,
				Step:       run.CurrentStep,
				ActionType: action.Type,
				ToolName:   action.ToolName,
				Error:      redact.Error(err),
			})
			decision := run.Deps.ToolErrorPolicy.Decide(ctx, action.ToolName, err)
			if decision.Continue() {
				run.ActionResult = toolErrText
				run.ActionDone = false
				run.AppendCalledTool(run.ToolInvoked)
				return core.Continue(), nil
			}
			return core.StageResult{}, err
		}
		if err := run.Guardrails.RecordToolCall(len(toolResult.Output)); err != nil {
			return core.StageResult{}, err
		}
		if err := run.Memory.AddToolResult(ctx, action.ToolName, toolResult.Output); err != nil {
			return core.StageResult{}, err
		}
		run.Notify(ctx, core.Event{
			Type:       core.EventToolComplete,
			Step:       run.CurrentStep,
			ActionType: action.Type,
			ToolName:   action.ToolName,
		})
		run.AppendCalledTool(run.ToolInvoked)
		run.ActionResult = toolResult.Output
		run.ActionDone = run.NextAction.Done
		if run.NextAction.Done {
			if err := run.Memory.AddAssistantMessage(ctx, toolResult.Output); err != nil {
				return core.StageResult{}, err
			}
		}
		if err := run.ExecutePhase(ctx, core.PhaseAfterToolExecution, nil); err != nil {
			return core.StageResult{}, err
		}
		return core.Continue(), nil
	default:
		return core.StageResult{}, apperrors.New(
			apperrors.CodeValidation,
			fmt.Sprintf("unsupported action type: %s", action.Type),
			false,
		)
	}
}

package stages

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/blanergol/agent-core/core"
)

type actTestMemory struct {
	toolResults int
}

func (m *actTestMemory) NewRun() core.Memory                               { return m }
func (m *actTestMemory) AddUserMessage(context.Context, string) error      { return nil }
func (m *actTestMemory) AddAssistantMessage(context.Context, string) error { return nil }
func (m *actTestMemory) AddToolResult(context.Context, string, string) error {
	m.toolResults++
	return nil
}
func (m *actTestMemory) AddSystemMessage(context.Context, string) error { return nil }
func (m *actTestMemory) BuildContext(context.Context, string) ([]core.Message, error) {
	return nil, nil
}
func (m *actTestMemory) ShortTermSnapshot() []core.Message { return nil }
func (m *actTestMemory) RestoreShortTerm([]core.Message)   {}

type actTestGuardrails struct{}

func (g *actTestGuardrails) NewRun() core.Guardrails { return g }
func (g *actTestGuardrails) BeforeStep() error       { return nil }
func (g *actTestGuardrails) ValidateAction(core.Action) error {
	return nil
}
func (g *actTestGuardrails) RecordToolCall(int) error { return nil }
func (g *actTestGuardrails) Stats() (int, int, time.Duration) {
	return 1, 0, 0
}
func (g *actTestGuardrails) Snapshot() core.GuardrailsSnapshot { return core.GuardrailsSnapshot{} }
func (g *actTestGuardrails) Restore(core.GuardrailsSnapshot)   {}

type actTestTools struct {
	readOnly map[string]bool
}

func (t *actTestTools) Specs() []core.ToolSpec { return nil }
func (t *actTestTools) Execute(context.Context, string, json.RawMessage) (core.ToolResult, error) {
	return core.ToolResult{}, nil
}
func (t *actTestTools) IsReadOnlyTool(name string) (bool, bool) {
	value, ok := t.readOnly[name]
	return value, ok
}

type actTestExecutor struct {
	calls int
}

func (e *actTestExecutor) Execute(_ context.Context, _ *core.RunContext, _ core.ToolCall) (core.ToolResult, error) {
	e.calls++
	return core.ToolResult{Output: `{"ok":true}`}, nil
}

// TestActStageRequiresApprovalForMutatingTools проверяет pending approval для mutating tool.
func TestActStageRequiresApprovalForMutatingTools(t *testing.T) {
	stage := NewActStage()
	mem := &actTestMemory{}
	tools := &actTestTools{readOnly: map[string]bool{"kv.put": false}}
	exec := &actTestExecutor{}
	run := &core.RunContext{
		Meta: core.RunMeta{SessionID: "session-1"},
		Config: core.RuntimeConfig{
			RequireToolApproval: true,
		},
		Memory:     mem,
		Guardrails: &actTestGuardrails{},
		Deps: core.RuntimeDeps{
			Tools:           tools,
			ToolExecutor:    exec,
			ToolErrorPolicy: core.NewStaticToolErrorPolicy(core.ToolErrorModeFail, nil),
		},
		State: core.NewAgentState("update state"),
		NextAction: core.NextAction{
			Done: false,
			Action: core.Action{
				Type:     core.ActionTypeTool,
				ToolName: "kv.put",
				ToolArgs: json.RawMessage(`{"key":"x","value":"y"}`),
			},
		},
	}

	if _, err := stage.Run(context.Background(), run); err != nil {
		t.Fatalf("act stage run: %v", err)
	}
	if run.PendingApproval == nil {
		t.Fatalf("expected pending approval to be created")
	}
	if run.PendingStopReason != core.StopReasonAwaitingHumanApproval {
		t.Fatalf("stop reason = %s, want %s", run.PendingStopReason, core.StopReasonAwaitingHumanApproval)
	}
	if exec.calls != 0 {
		t.Fatalf("tool executor calls = %d, want 0", exec.calls)
	}
}

// TestActStageSkipsApprovalOnceAfterApprove проверяет одноразовый bypass approval после approve.
func TestActStageSkipsApprovalOnceAfterApprove(t *testing.T) {
	stage := NewActStage()
	mem := &actTestMemory{}
	tools := &actTestTools{readOnly: map[string]bool{"kv.put": false}}
	exec := &actTestExecutor{}
	run := &core.RunContext{
		Meta: core.RunMeta{SessionID: "session-1"},
		Config: core.RuntimeConfig{
			RequireToolApproval: true,
		},
		Memory:     mem,
		Guardrails: &actTestGuardrails{},
		Deps: core.RuntimeDeps{
			Tools:           tools,
			ToolExecutor:    exec,
			ToolErrorPolicy: core.NewStaticToolErrorPolicy(core.ToolErrorModeFail, nil),
		},
		State:            core.NewAgentState("update state"),
		SkipApprovalOnce: true,
		NextAction: core.NextAction{
			Done: false,
			Action: core.Action{
				Type:     core.ActionTypeTool,
				ToolName: "kv.put",
				ToolArgs: json.RawMessage(`{"key":"x","value":"y"}`),
			},
		},
	}

	if _, err := stage.Run(context.Background(), run); err != nil {
		t.Fatalf("act stage run: %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("tool executor calls = %d, want 1", exec.calls)
	}
	if run.PendingApproval != nil {
		t.Fatalf("unexpected pending approval after approved resume")
	}
}

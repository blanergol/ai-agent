package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type runtimeTestMemory struct {
	short []Message
}

func (m *runtimeTestMemory) NewRun() Memory                                          { return m }
func (m *runtimeTestMemory) AddUserMessage(context.Context, string) error            { return nil }
func (m *runtimeTestMemory) AddAssistantMessage(context.Context, string) error       { return nil }
func (m *runtimeTestMemory) AddToolResult(context.Context, string, string) error     { return nil }
func (m *runtimeTestMemory) AddSystemMessage(context.Context, string) error          { return nil }
func (m *runtimeTestMemory) BuildContext(context.Context, string) ([]Message, error) { return nil, nil }
func (m *runtimeTestMemory) ShortTermSnapshot() []Message {
	out := make([]Message, len(m.short))
	copy(out, m.short)
	return out
}
func (m *runtimeTestMemory) RestoreShortTerm(messages []Message) {
	m.short = make([]Message, len(messages))
	copy(m.short, messages)
}

type runtimeTestGuardrails struct {
	snapshot GuardrailsSnapshot
}

func (g *runtimeTestGuardrails) NewRun() Guardrails          { return g }
func (g *runtimeTestGuardrails) BeforeStep() error           { return nil }
func (g *runtimeTestGuardrails) ValidateAction(Action) error { return nil }
func (g *runtimeTestGuardrails) RecordToolCall(int) error    { return nil }
func (g *runtimeTestGuardrails) Stats() (int, int, time.Duration) {
	return g.snapshot.Steps, g.snapshot.ToolCalls, g.snapshot.Elapsed
}
func (g *runtimeTestGuardrails) Snapshot() GuardrailsSnapshot { return g.snapshot }
func (g *runtimeTestGuardrails) Restore(snapshot GuardrailsSnapshot) {
	g.snapshot = snapshot
}

type runtimeTestSnapshotStore struct {
	snapshot RuntimeSnapshot
	ok       bool
}

func (s *runtimeTestSnapshotStore) Save(_ context.Context, snapshot RuntimeSnapshot) error {
	s.snapshot = snapshot
	s.ok = true
	return nil
}

func (s *runtimeTestSnapshotStore) Load(_ context.Context, _ string) (RuntimeSnapshot, bool, error) {
	if !s.ok {
		return RuntimeSnapshot{}, false, nil
	}
	return s.snapshot, true, nil
}

func TestRuntimeHandlePendingApproval(t *testing.T) {
	rt := &Runtime{}
	run := &RunContext{
		PendingApproval: &PendingToolApproval{
			RequestID: "apr-1",
			Action: Action{
				Type:     ActionTypeTool,
				ToolName: "kv.put",
				ToolArgs: json.RawMessage(`{"key":"x"}`),
			},
			Done: true,
		},
	}

	handled, result, err := rt.handlePendingApproval(context.Background(), run)
	if err != nil {
		t.Fatalf("handle pending (no decision): %v", err)
	}
	if !handled {
		t.Fatalf("handled = %t, want true", handled)
	}
	if result.StopReason != StopReasonAwaitingHumanApproval {
		t.Fatalf("stop reason = %s, want %s", result.StopReason, StopReasonAwaitingHumanApproval)
	}

	run = &RunContext{
		PendingApproval: &PendingToolApproval{
			RequestID: "apr-1",
			Action: Action{
				Type:     ActionTypeTool,
				ToolName: "kv.put",
				ToolArgs: json.RawMessage(`{"key":"x"}`),
			},
			Done: true,
		},
		Approval: &ApprovalInput{
			RequestID: "apr-1",
			Decision:  ApprovalDecisionApprove,
		},
	}
	handled, _, err = rt.handlePendingApproval(context.Background(), run)
	if err != nil {
		t.Fatalf("handle pending approve: %v", err)
	}
	if handled {
		t.Fatalf("handled = %t, want false for approved action resume", handled)
	}
	if run.ResumeAction == nil || run.ResumeAction.ToolName != "kv.put" {
		t.Fatalf("resume action = %#v", run.ResumeAction)
	}
	if !run.SkipApprovalOnce {
		t.Fatalf("skip approval flag = %t, want true", run.SkipApprovalOnce)
	}

	run = &RunContext{
		PendingApproval: &PendingToolApproval{
			RequestID: "apr-2",
			Action:    Action{Type: ActionTypeTool, ToolName: "kv.put"},
		},
		Approval: &ApprovalInput{
			RequestID: "apr-2",
			Decision:  ApprovalDecisionDeny,
			Comment:   "manual reject",
		},
	}
	handled, result, err = rt.handlePendingApproval(context.Background(), run)
	if err != nil {
		t.Fatalf("handle pending deny: %v", err)
	}
	if !handled {
		t.Fatalf("handled = %t, want true", handled)
	}
	if result.StopReason != StopReasonApprovalDenied {
		t.Fatalf("stop reason = %s, want %s", result.StopReason, StopReasonApprovalDenied)
	}
}

func TestRuntimeSnapshotPersistsAndRestoresPendingState(t *testing.T) {
	snapshots := &runtimeTestSnapshotStore{}
	rt := &Runtime{
		cfg: RuntimeConfig{
			SnapshotTimeout: time.Second,
		},
		deps: RuntimeDeps{
			SnapshotStore: snapshots,
		},
	}

	mem := &runtimeTestMemory{
		short: []Message{{Role: RoleUser, Content: "hello"}},
	}
	guardrails := &runtimeTestGuardrails{
		snapshot: GuardrailsSnapshot{
			Steps:     3,
			ToolCalls: 1,
			Elapsed:   2 * time.Second,
		},
	}
	run := &RunContext{
		Meta:                 RunMeta{SessionID: "session-1"},
		Memory:               mem,
		Guardrails:           guardrails,
		PendingStop:          true,
		PendingStopReason:    StopReasonAwaitingHumanApproval,
		PendingFinalResponse: "approval required",
		PendingApproval: &PendingToolApproval{
			RequestID: "apr-1",
			Action:    Action{Type: ActionTypeTool, ToolName: "kv.put"},
		},
		PlanningSteps: []PlanningStep{{Step: 1, ActionType: "tool", ToolName: "kv.put"}},
		CalledTools:   []string{"kv.put"},
		State: &AgentState{
			Context:       map[string]any{"k": "v"},
			RetrievedDocs: []string{"doc-1"},
		},
	}
	rt.persistSnapshot(context.Background(), run)

	restoreMem := &runtimeTestMemory{}
	restoreGuardrails := &runtimeTestGuardrails{}
	restoreRun := &RunContext{
		Meta:       RunMeta{SessionID: "session-1"},
		Memory:     restoreMem,
		Guardrails: restoreGuardrails,
		State:      NewAgentState("restored"),
	}
	rt.restoreSnapshot(context.Background(), restoreRun)

	if len(restoreMem.short) != 1 || restoreMem.short[0].Content != "hello" {
		t.Fatalf("restored short-term = %#v", restoreMem.short)
	}
	if restoreGuardrails.snapshot.Steps != 3 {
		t.Fatalf("restored guardrails steps = %d, want 3", restoreGuardrails.snapshot.Steps)
	}
	if restoreRun.PendingApproval == nil || restoreRun.PendingApproval.RequestID != "apr-1" {
		t.Fatalf("restored pending approval = %#v", restoreRun.PendingApproval)
	}
	if restoreRun.PendingStopReason != StopReasonAwaitingHumanApproval {
		t.Fatalf("restored stop reason = %s", restoreRun.PendingStopReason)
	}
	if len(restoreRun.PlanningSteps) != 1 || restoreRun.PlanningSteps[0].ToolName != "kv.put" {
		t.Fatalf("restored planning steps = %#v", restoreRun.PlanningSteps)
	}
	if len(restoreRun.CalledTools) != 1 || restoreRun.CalledTools[0] != "kv.put" {
		t.Fatalf("restored called tools = %#v", restoreRun.CalledTools)
	}
	if restoreRun.State.Context["k"] != "v" {
		t.Fatalf("restored context = %#v", restoreRun.State.Context)
	}
	if len(restoreRun.State.RetrievedDocs) != 1 || restoreRun.State.RetrievedDocs[0] != "doc-1" {
		t.Fatalf("restored retrieved docs = %#v", restoreRun.State.RetrievedDocs)
	}
}

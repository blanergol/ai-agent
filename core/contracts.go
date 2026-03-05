package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Planner chooses the next action for a run loop.
type Planner interface {
	Plan(ctx context.Context, obs Observation) (NextAction, error)
}

// ToolRegistry exposes tool catalog and execution.
type ToolRegistry interface {
	Specs() []ToolSpec
	Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
}

// Memory is a run-scoped memory contract.
type Memory interface {
	NewRun() Memory
	AddUserMessage(ctx context.Context, text string) error
	AddAssistantMessage(ctx context.Context, text string) error
	AddToolResult(ctx context.Context, toolName, result string) error
	AddSystemMessage(ctx context.Context, text string) error
	BuildContext(ctx context.Context, userInput string) ([]Message, error)
	ShortTermSnapshot() []Message
	RestoreShortTerm(messages []Message)
}

// Guardrails controls run limits and action safety.
type Guardrails interface {
	NewRun() Guardrails
	BeforeStep() error
	ValidateAction(action Action) error
	RecordToolCall(outputSize int) error
	Stats() (steps int, toolCalls int, elapsed time.Duration)
	Snapshot() GuardrailsSnapshot
	Restore(snapshot GuardrailsSnapshot)
}

// StateSnapshotter exposes session-scoped state snapshots for planner context.
type StateSnapshotter interface {
	SnapshotForSession(ctx context.Context) map[string]any
}

// OutputValidator validates final response safety/contract.
type OutputValidator interface {
	Validate(ctx context.Context, text string) error
}

// SnapshotStore persists minimal runtime state for session continuation.
type SnapshotStore interface {
	Save(ctx context.Context, snapshot RuntimeSnapshot) error
	Load(ctx context.Context, sessionID string) (RuntimeSnapshot, bool, error)
}

// ToolErrorMode defines tool-error handling strategy.
type ToolErrorMode string

const (
	ToolErrorModeFail     ToolErrorMode = "fail"
	ToolErrorModeContinue ToolErrorMode = "continue"
)

// ToolErrorDecision is tool-error policy decision.
type ToolErrorDecision struct {
	Mode ToolErrorMode
}

// Continue returns true when execution should continue after tool error.
func (d ToolErrorDecision) Continue() bool {
	return d.Mode == ToolErrorModeContinue
}

// ToolErrorPolicy decides whether to continue/fail on tool errors.
type ToolErrorPolicy interface {
	Decide(ctx context.Context, toolName string, err error) ToolErrorDecision
}

// Observer receives normalized runtime events.
type Observer interface {
	OnEvent(ctx context.Context, event Event)
}

// ContextBinder binds session/correlation context to request context.
type ContextBinder interface {
	Ensure(meta RunMeta) RunMeta
	Bind(ctx context.Context, meta RunMeta) context.Context
}

type noopOutputValidator struct{}

func (noopOutputValidator) Validate(_ context.Context, _ string) error { return nil }

type noopObserver struct{}

func (noopObserver) OnEvent(_ context.Context, _ Event) {}

type noopSnapshotStore struct{}

func (noopSnapshotStore) Save(_ context.Context, _ RuntimeSnapshot) error { return nil }
func (noopSnapshotStore) Load(_ context.Context, _ string) (RuntimeSnapshot, bool, error) {
	return RuntimeSnapshot{}, false, nil
}

type staticToolErrorPolicy struct {
	defaultMode ToolErrorMode
	perTool     map[string]ToolErrorMode
}

// NewStaticToolErrorPolicy creates default + per-tool tool-error policy.
func NewStaticToolErrorPolicy(defaultMode ToolErrorMode, perTool map[string]ToolErrorMode) ToolErrorPolicy {
	mode := normalizeToolErrorMode(defaultMode)
	overrides := make(map[string]ToolErrorMode, len(perTool))
	for tool, rawMode := range perTool {
		key := normalizeToolName(tool)
		if key == "" {
			continue
		}
		overrides[key] = normalizeToolErrorMode(rawMode)
	}
	return &staticToolErrorPolicy{
		defaultMode: mode,
		perTool:     overrides,
	}
}

// ParseToolErrorMode parses configured mode string.
func ParseToolErrorMode(raw string) (ToolErrorMode, error) {
	mode := normalizeToolErrorMode(ToolErrorMode(raw))
	switch mode {
	case ToolErrorModeFail, ToolErrorModeContinue:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported tool error mode: %s", raw)
	}
}

func (p *staticToolErrorPolicy) Decide(_ context.Context, toolName string, _ error) ToolErrorDecision {
	if p == nil {
		return ToolErrorDecision{Mode: ToolErrorModeFail}
	}
	key := normalizeToolName(toolName)
	if mode, ok := p.perTool[key]; ok {
		return ToolErrorDecision{Mode: mode}
	}
	return ToolErrorDecision{Mode: p.defaultMode}
}

func normalizeToolErrorMode(mode ToolErrorMode) ToolErrorMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(ToolErrorModeContinue):
		return ToolErrorModeContinue
	case string(ToolErrorModeFail):
		return ToolErrorModeFail
	default:
		return ToolErrorModeFail
	}
}

func normalizeToolName(toolName string) string {
	return strings.ToLower(strings.TrimSpace(toolName))
}

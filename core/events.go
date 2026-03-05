package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/blanergol/agent-core/pkg/redact"
)

// EventType is runtime event category.
type EventType string

const (
	EventRunStarted      EventType = "run_started"
	EventPhaseStarted    EventType = "phase_started"
	EventPhaseCompleted  EventType = "phase_completed"
	EventStepPlanned     EventType = "step_planned"
	EventToolComplete    EventType = "tool_completed"
	EventToolFailed      EventType = "tool_failed"
	EventToolTraced      EventType = "tool_traced"
	EventIterationMetric EventType = "iteration_metric"
	EventOutputInvalid   EventType = "output_invalid"
	EventRunCompleted    EventType = "run_completed"
	EventRunFailed       EventType = "run_failed"
)

// Event is an observable runtime lifecycle event.
type Event struct {
	Type          EventType
	Timestamp     time.Time
	SessionID     string
	CorrelationID string
	UserSub       string

	Step       int
	Iteration  int
	Phase      Phase
	DurationMs int64
	ActionType string
	ToolName   string
	StopReason string
	InputHash  string
	OutputHash string
	Error      string
}

// Notify dispatches sanitized event to observer.
func (r *RunContext) Notify(ctx context.Context, event Event) {
	observer := r.Deps.Observer
	if observer == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if strings.TrimSpace(event.SessionID) == "" {
		event.SessionID = r.Meta.SessionID
	}
	if strings.TrimSpace(event.CorrelationID) == "" {
		event.CorrelationID = r.Meta.CorrelationID
	}
	if strings.TrimSpace(event.UserSub) == "" {
		event.UserSub = r.Meta.UserSub
	}
	event.UserSub = UserSubForLogs(event.UserSub)
	event.Error = redact.Text(event.Error)
	event.StopReason = redact.Text(event.StopReason)
	observer.OnEvent(ctx, event)
}

// UserSubForLogs returns irreversible stable hash for logs/events.
func UserSubForLogs(userSub string) string {
	userSub = strings.TrimSpace(userSub)
	if userSub == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(userSub))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

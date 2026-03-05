package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// StopReasonAwaitingHumanApproval сообщает, что выполнение ждет внешнего approve/deny.
	StopReasonAwaitingHumanApproval = "awaiting_human_approval"
	// StopReasonApprovalDenied сообщает, что mutating действие было отклонено человеком.
	StopReasonApprovalDenied = "approval_denied"
)

const (
	// ApprovalDecisionApprove разрешает выполнение ожидающего mutating-действия.
	ApprovalDecisionApprove = "approve"
	// ApprovalDecisionDeny отклоняет выполнение ожидающего mutating-действия.
	ApprovalDecisionDeny = "deny"
)

// ApprovalInput описывает решение человека по ожидающему mutating-действию.
type ApprovalInput struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"`
	Comment   string `json:"comment,omitempty"`
}

// PendingToolApproval описывает ожидающее подтверждение mutating tool call.
type PendingToolApproval struct {
	RequestID   string    `json:"request_id"`
	Action      Action    `json:"action"`
	Done        bool      `json:"done"`
	Reason      string    `json:"reason,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

// Normalize очищает и нормализует поля approval payload.
func (a *ApprovalInput) Normalize() {
	if a == nil {
		return
	}
	a.RequestID = strings.TrimSpace(a.RequestID)
	a.Decision = strings.ToLower(strings.TrimSpace(a.Decision))
	a.Comment = strings.TrimSpace(a.Comment)
}

// IsValidDecision проверяет, что decision принадлежит поддерживаемому набору.
func (a *ApprovalInput) IsValidDecision() bool {
	if a == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(a.Decision)) {
	case ApprovalDecisionApprove, ApprovalDecisionDeny:
		return true
	default:
		return false
	}
}

// NewPendingToolApproval формирует стабильный pending-объект для последующего approve/deny workflow.
func NewPendingToolApproval(sessionID string, step int, action Action, done bool, reason string) *PendingToolApproval {
	raw, _ := json.Marshal(map[string]any{
		"session": sessionID,
		"step":    step,
		"tool":    strings.TrimSpace(action.ToolName),
		"args":    json.RawMessage(action.ToolArgs),
		"ts":      time.Now().UTC().UnixNano(),
	})
	sum := sha256.Sum256(raw)
	return &PendingToolApproval{
		RequestID:   fmt.Sprintf("apr-%s", hex.EncodeToString(sum[:8])),
		Action:      action,
		Done:        done,
		Reason:      strings.TrimSpace(reason),
		RequestedAt: time.Now().UTC(),
	}
}

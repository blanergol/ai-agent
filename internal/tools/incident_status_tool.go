package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	toolkit "github.com/blanergol/agent-core/pkg/tools"
)

const (
	IncidentStatusToolName = ".incident.status"
)

// IncidentStatusTool reads incident state from private in-memory store.
type IncidentStatusTool struct {
	Store *IncidentStore
}

var _ toolkit.Tool = (*IncidentStatusTool)(nil)
var _ toolkit.RetryPolicy = (*IncidentStatusTool)(nil)
var _ toolkit.CachePolicy = (*IncidentStatusTool)(nil)

func NewIncidentStatusTool(store *IncidentStore) *IncidentStatusTool {
	if store == nil {
		store = NewIncidentStore()
	}
	return &IncidentStatusTool{Store: store}
}

func (t *IncidentStatusTool) Name() string { return IncidentStatusToolName }

func (t *IncidentStatusTool) Description() string {
	return "Returns current incident status, assignee and timeline summary."
}

func (t *IncidentStatusTool) InputSchema() string {
	return `{"type":"object","required":["incident_id"],"properties":{"incident_id":{"type":"string","minLength":1}},"additionalProperties":false}`
}

func (t *IncidentStatusTool) OutputSchema() string {
	return `{"type":"object","required":["incident_id","service","severity","status","assignee_team","runbook_id","created_at","updated_at","timeline_count","last_event"],"properties":{"incident_id":{"type":"string"},"service":{"type":"string"},"severity":{"type":"string"},"status":{"type":"string"},"assignee_team":{"type":"string"},"runbook_id":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"},"timeline_count":{"type":"integer"},"last_event":{"type":"string"}},"additionalProperties":false}`
}

func (t *IncidentStatusTool) IsReadOnly() bool { return true }

func (t *IncidentStatusTool) IsSafeRetry() bool { return true }

func (t *IncidentStatusTool) CacheTTL() time.Duration { return 10 * time.Second }

func (t *IncidentStatusTool) Execute(_ context.Context, args json.RawMessage) (toolkit.ToolResult, error) {
	var input struct {
		IncidentID string `json:"incident_id"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return toolkit.ToolResult{}, err
	}
	incidentID := strings.TrimSpace(input.IncidentID)
	if incidentID == "" {
		return toolkit.ToolResult{}, fmt.Errorf("incident_id is required")
	}

	incident, ok := t.Store.GetIncident(incidentID)
	if !ok {
		return toolkit.ToolResult{}, fmt.Errorf("incident not found: %s", incidentID)
	}

	payload := incidentStatusPayload(incident)
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	return toolkit.ToolResult{Output: string(raw)}, nil
}

func incidentStatusPayload(incident Incident) map[string]any {
	lastEvent := ""
	if size := len(incident.Timeline); size > 0 {
		last := incident.Timeline[size-1]
		lastEvent = strings.TrimSpace(last.Type + ": " + last.Message)
	}
	return map[string]any{
		"incident_id":    incident.ID,
		"service":        incident.Service,
		"severity":       incident.Severity,
		"status":         incident.Status,
		"assignee_team":  incident.AssigneeTeam,
		"runbook_id":     incident.RunbookID,
		"created_at":     incident.CreatedAt.Format(time.RFC3339),
		"updated_at":     incident.UpdatedAt.Format(time.RFC3339),
		"timeline_count": len(incident.Timeline),
		"last_event":     lastEvent,
	}
}

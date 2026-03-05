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
	IncidentUpdateToolName = ".incident.update"
)

// IncidentUpdateTool updates mutable incident fields and timeline.
type IncidentUpdateTool struct {
	Store *IncidentStore
}

var _ toolkit.Tool = (*IncidentUpdateTool)(nil)
var _ toolkit.RetryPolicy = (*IncidentUpdateTool)(nil)

func NewIncidentUpdateTool(store *IncidentStore) *IncidentUpdateTool {
	if store == nil {
		store = NewIncidentStore()
	}
	return &IncidentUpdateTool{Store: store}
}

func (t *IncidentUpdateTool) Name() string { return IncidentUpdateToolName }

func (t *IncidentUpdateTool) Description() string {
	return "Updates incident status/assignee and appends deterministic timeline notes."
}

func (t *IncidentUpdateTool) InputSchema() string {
	return `{"type":"object","required":["incident_id"],"properties":{"incident_id":{"type":"string","minLength":1},"status":{"type":"string","enum":["investigating","mitigating","monitoring","resolved"]},"note":{"type":"string"},"next_action":{"type":"string"},"assignee_team":{"type":"string"}},"additionalProperties":false}`
}

func (t *IncidentUpdateTool) OutputSchema() string {
	return `{"type":"object","required":["incident_id","service","severity","status","assignee_team","runbook_id","created_at","updated_at","timeline_count","last_event"],"properties":{"incident_id":{"type":"string"},"service":{"type":"string"},"severity":{"type":"string"},"status":{"type":"string"},"assignee_team":{"type":"string"},"runbook_id":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"},"timeline_count":{"type":"integer"},"last_event":{"type":"string"}},"additionalProperties":false}`
}

func (t *IncidentUpdateTool) IsReadOnly() bool { return false }

func (t *IncidentUpdateTool) IsSafeRetry() bool { return false }

func (t *IncidentUpdateTool) Execute(_ context.Context, args json.RawMessage) (toolkit.ToolResult, error) {
	var input struct {
		IncidentID   string `json:"incident_id"`
		Status       string `json:"status"`
		Note         string `json:"note"`
		NextAction   string `json:"next_action"`
		AssigneeTeam string `json:"assignee_team"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return toolkit.ToolResult{}, err
	}
	incidentID := strings.TrimSpace(input.IncidentID)
	if incidentID == "" {
		return toolkit.ToolResult{}, fmt.Errorf("incident_id is required")
	}

	incident, err := t.Store.UpdateIncident(
		UpdateIncidentInput{
			IncidentID:   incidentID,
			Status:       input.Status,
			Note:         input.Note,
			NextAction:   input.NextAction,
			AssigneeTeam: input.AssigneeTeam,
		},
		time.Now().UTC(),
	)
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	raw, err := json.Marshal(incidentStatusPayload(incident))
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	return toolkit.ToolResult{
		Output: string(raw),
		Metadata: map[string]any{
			"_domain":     "incident_response",
			"incident_id": incident.ID,
			"updated":     true,
		},
	}, nil
}

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
	IncidentCreateToolName = ".incident.create"
)

// IncidentCreateTool creates incident records in the private in-memory store.
type IncidentCreateTool struct {
	Store *IncidentStore
}

var _ toolkit.Tool = (*IncidentCreateTool)(nil)
var _ toolkit.RetryPolicy = (*IncidentCreateTool)(nil)

func NewIncidentCreateTool(store *IncidentStore) *IncidentCreateTool {
	if store == nil {
		store = NewIncidentStore()
	}
	return &IncidentCreateTool{Store: store}
}

func (t *IncidentCreateTool) Name() string { return IncidentCreateToolName }

func (t *IncidentCreateTool) Description() string {
	return "Creates a new incident record and initializes timeline for response orchestration."
}

func (t *IncidentCreateTool) InputSchema() string {
	return `{"type":"object","required":["service","summary"],"properties":{"service":{"type":"string","minLength":1},"summary":{"type":"string","minLength":1},"severity":{"type":"string","enum":["sev1","sev2","sev3","sev4"]},"source":{"type":"string"},"assignee_team":{"type":"string"},"customer_impact":{"type":"boolean"}},"additionalProperties":false}`
}

func (t *IncidentCreateTool) OutputSchema() string {
	return `{"type":"object","required":["incident_id","service","severity","status","summary","assignee_team","runbook_id","source","created_at","updated_at"],"properties":{"incident_id":{"type":"string"},"service":{"type":"string"},"severity":{"type":"string"},"status":{"type":"string"},"summary":{"type":"string"},"assignee_team":{"type":"string"},"runbook_id":{"type":"string"},"source":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"}},"additionalProperties":false}`
}

func (t *IncidentCreateTool) IsReadOnly() bool { return false }

func (t *IncidentCreateTool) IsSafeRetry() bool { return false }

func (t *IncidentCreateTool) Execute(_ context.Context, args json.RawMessage) (toolkit.ToolResult, error) {
	var input struct {
		Service        string `json:"service"`
		Summary        string `json:"summary"`
		Severity       string `json:"severity"`
		Source         string `json:"source"`
		AssigneeTeam   string `json:"assignee_team"`
		CustomerImpact bool   `json:"customer_impact"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return toolkit.ToolResult{}, err
	}
	service := strings.TrimSpace(input.Service)
	summary := strings.TrimSpace(input.Summary)
	if service == "" || summary == "" {
		return toolkit.ToolResult{}, fmt.Errorf("service and summary are required")
	}

	incident, err := t.Store.CreateIncident(
		CreateIncidentInput{
			Service:        service,
			Summary:        summary,
			Severity:       input.Severity,
			Source:         input.Source,
			AssigneeTeam:   input.AssigneeTeam,
			CustomerImpact: input.CustomerImpact,
		},
		time.Now().UTC(),
	)
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	raw, err := json.Marshal(incidentPayload(incident))
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	return toolkit.ToolResult{
		Output: string(raw),
		Metadata: map[string]any{
			"_domain":      "incident_response",
			"incident_id":  incident.ID,
			"service":      incident.Service,
			"incident_new": true,
		},
	}, nil
}

func incidentPayload(incident Incident) map[string]any {
	return map[string]any{
		"incident_id":   incident.ID,
		"service":       incident.Service,
		"severity":      incident.Severity,
		"status":        incident.Status,
		"summary":       incident.Summary,
		"assignee_team": incident.AssigneeTeam,
		"runbook_id":    incident.RunbookID,
		"source":        incident.Source,
		"created_at":    incident.CreatedAt.Format(time.RFC3339),
		"updated_at":    incident.UpdatedAt.Format(time.RFC3339),
	}
}

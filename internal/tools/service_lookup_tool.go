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
	ServiceLookupToolName = ".service.lookup"
)

// ServiceLookupTool resolves deterministic metadata about production services.
type ServiceLookupTool struct {
	Store *IncidentStore
}

var _ toolkit.Tool = (*ServiceLookupTool)(nil)
var _ toolkit.RetryPolicy = (*ServiceLookupTool)(nil)
var _ toolkit.CachePolicy = (*ServiceLookupTool)(nil)

// NewServiceLookupTool creates read-only service catalog lookup tool.
func NewServiceLookupTool(store *IncidentStore) *ServiceLookupTool {
	if store == nil {
		store = NewIncidentStore()
	}
	return &ServiceLookupTool{Store: store}
}

func (t *ServiceLookupTool) Name() string { return ServiceLookupToolName }

func (t *ServiceLookupTool) Description() string {
	return "Returns service ownership, runbook and impact metadata for incident triage."
}

func (t *ServiceLookupTool) InputSchema() string {
	return `{"type":"object","required":["service"],"properties":{"service":{"type":"string","minLength":1}},"additionalProperties":false}`
}

func (t *ServiceLookupTool) OutputSchema() string {
	return `{"type":"object","required":["service","tier","owner_team","pagerduty_service","runbook_id","business_impact","dependencies","default_severity"],"properties":{"service":{"type":"string"},"tier":{"type":"string"},"owner_team":{"type":"string"},"pagerduty_service":{"type":"string"},"runbook_id":{"type":"string"},"business_impact":{"type":"string"},"dependencies":{"type":"array","items":{"type":"string"}},"default_severity":{"type":"string"}},"additionalProperties":false}`
}

func (t *ServiceLookupTool) IsReadOnly() bool { return true }

func (t *ServiceLookupTool) IsSafeRetry() bool { return true }

func (t *ServiceLookupTool) CacheTTL() time.Duration { return 2 * time.Minute }

func (t *ServiceLookupTool) Execute(_ context.Context, args json.RawMessage) (toolkit.ToolResult, error) {
	var input struct {
		Service string `json:"service"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return toolkit.ToolResult{}, err
	}
	service := strings.TrimSpace(input.Service)
	if service == "" {
		return toolkit.ToolResult{}, fmt.Errorf("service is required")
	}

	profile, err := t.Store.LookupService(service)
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	payload := map[string]any{
		"service":           profile.Service,
		"tier":              profile.Tier,
		"owner_team":        profile.OwnerTeam,
		"pagerduty_service": profile.PagerDuty,
		"runbook_id":        profile.RunbookID,
		"business_impact":   profile.BusinessImpact,
		"dependencies":      profile.Dependencies,
		"default_severity":  profile.DefaultSeverity,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolkit.ToolResult{}, err
	}
	return toolkit.ToolResult{Output: string(raw)}, nil
}

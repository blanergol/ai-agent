package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestServiceLookupToolExecute(t *testing.T) {
	tool := NewServiceLookupTool(NewIncidentStore())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"service":"payments-api"}`))
	if err != nil {
		t.Fatalf("execute service lookup: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if payload["service"] != "payments-api" {
		t.Fatalf("service = %v, want payments-api", payload["service"])
	}
	if payload["runbook_id"] == "" {
		t.Fatalf("runbook_id should not be empty")
	}
}

func TestRunbookLookupToolFallsBackToDefaultScenario(t *testing.T) {
	tool := NewRunbookLookupTool(NewIncidentStore())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"runbook_id":"rb-payments-latency","scenario":"missing"}`))
	if err != nil {
		t.Fatalf("execute runbook lookup: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if payload["scenario"] != "high-latency" {
		t.Fatalf("scenario = %v, want high-latency", payload["scenario"])
	}
}

func TestOnCallLookupToolExecute(t *testing.T) {
	tool := NewOnCallLookupTool(NewIncidentStore())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"team":"identity-platform"}`))
	if err != nil {
		t.Fatalf("execute oncall lookup: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if payload["team"] != "identity-platform" {
		t.Fatalf("team = %v", payload["team"])
	}
}

func TestIncidentLifecycleTools(t *testing.T) {
	store := NewIncidentStore()
	createTool := NewIncidentCreateTool(store)
	statusTool := NewIncidentStatusTool(store)
	updateTool := NewIncidentUpdateTool(store)

	createResult, err := createTool.Execute(
		context.Background(),
		json.RawMessage(`{"service":"auth-gateway","summary":"Login errors above 40%","severity":"sev1"}`),
	)
	if err != nil {
		t.Fatalf("execute incident create: %v", err)
	}

	var created map[string]any
	if err := json.Unmarshal([]byte(createResult.Output), &created); err != nil {
		t.Fatalf("decode create output: %v", err)
	}
	incidentID, _ := created["incident_id"].(string)
	if incidentID == "" {
		t.Fatalf("incident_id should not be empty")
	}

	statusResult, err := statusTool.Execute(
		context.Background(),
		json.RawMessage(`{"incident_id":"`+incidentID+`"}`),
	)
	if err != nil {
		t.Fatalf("execute incident status: %v", err)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(statusResult.Output), &status); err != nil {
		t.Fatalf("decode status output: %v", err)
	}
	if status["status"] != IncidentStatusInvestigating {
		t.Fatalf("status = %v, want investigating", status["status"])
	}

	updateResult, err := updateTool.Execute(
		context.Background(),
		json.RawMessage(`{"incident_id":"`+incidentID+`","status":"mitigating","note":"Traffic shifted to canary cluster"}`),
	)
	if err != nil {
		t.Fatalf("execute incident update: %v", err)
	}
	var updated map[string]any
	if err := json.Unmarshal([]byte(updateResult.Output), &updated); err != nil {
		t.Fatalf("decode update output: %v", err)
	}
	if updated["status"] != IncidentStatusMitigating {
		t.Fatalf("status = %v, want mitigating", updated["status"])
	}
	if updated["timeline_count"].(float64) < 2 {
		t.Fatalf("timeline_count should increase after update")
	}
}

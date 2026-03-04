package agent

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// goldenRunResult фиксирует сериализуемый контракт результата для golden-теста.
type goldenRunResult struct {
	APIVersion    string `json:"api_version"`
	SessionID     string `json:"session_id"`
	CorrelationID string `json:"correlation_id"`
	FinalResponse string `json:"final_response"`
	Steps         int    `json:"steps"`
	ToolCalls     int    `json:"tool_calls"`
	StopReason    string `json:"stop_reason"`
}

// TestGoldenRunResultDeterministic фиксирует стабильный контракт результата в детерминированном режиме.
func TestGoldenRunResultDeterministic(t *testing.T) {
	ag := newTestAgent(t, &flakyPlanner{}, RuntimeConfig{
		MaxStepTimeout: time.Second,
		Deterministic:  true,
	})

	res, err := ag.RunWithInput(context.Background(), RunInput{Text: "golden input"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	actual := goldenRunResult{
		APIVersion:    res.APIVersion,
		SessionID:     res.SessionID,
		CorrelationID: res.CorrelationID,
		FinalResponse: res.FinalResponse,
		Steps:         res.Steps,
		ToolCalls:     res.ToolCalls,
		StopReason:    res.StopReason,
	}
	actualJSON, err := json.MarshalIndent(actual, "", "  ")
	if err != nil {
		t.Fatalf("marshal actual: %v", err)
	}
	actualJSON = append(actualJSON, '\n')

	expectedJSON, err := os.ReadFile("testdata/run_result.golden.json")
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	if string(actualJSON) != string(expectedJSON) {
		t.Fatalf("golden mismatch\nexpected:\n%s\nactual:\n%s", string(expectedJSON), string(actualJSON))
	}
}

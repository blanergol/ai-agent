package llm

import (
	"encoding/json"
	"testing"
)

// TestExtractJSON_DecoratedPrefix проверяет извлечение JSON из ответа со служебным префиксом.
func TestExtractJSON_DecoratedPrefix(t *testing.T) {
	input := `<|channel|>final <|constrain|>JSON<|message|>{
  "done": true,
  "action": {
    "type": "final",
    "reasoning_summary": "x",
    "expected_outcome": "y",
    "final_response": "TEST_OK"
  }
}`

	raw, err := extractJSON(input)
	if err != nil {
		t.Fatalf("extractJSON error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal extracted json: %v", err)
	}
	if _, ok := parsed["done"]; !ok {
		t.Fatalf("expected field done in extracted json")
	}
}

// TestExtractJSON_MarkdownFence проверяет извлечение JSON из markdown fenced блока.
func TestExtractJSON_MarkdownFence(t *testing.T) {
	input := "```json\n{\"ok\":true}\n```"

	raw, err := extractJSON(input)
	if err != nil {
		t.Fatalf("extractJSON error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal extracted json: %v", err)
	}
	if parsed["ok"] != true {
		t.Fatalf("expected ok=true, got %v", parsed["ok"])
	}
}

// TestExtractJSON_Invalid проверяет ошибку при невалидном JSON-ответе.
func TestExtractJSON_Invalid(t *testing.T) {
	_, err := extractJSON("not-json")
	if err == nil {
		t.Fatalf("expected parse error for invalid json")
	}
}

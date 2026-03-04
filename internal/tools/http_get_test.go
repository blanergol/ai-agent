package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// TestHTTPGetRejectsDisallowedDomain проверяет блокировку домена вне allowlist.
func TestHTTPGetRejectsDisallowedDomain(t *testing.T) {
	// tool разрешает только example.com.
	tool := NewHTTPGetTool(HTTPGetConfig{AllowDomains: []string{"example.com"}})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"https://google.com"}`))
	if err == nil {
		t.Fatalf("expected domain allowlist error")
	}
}

// TestHTTPGetRejectsInternalIPEvenIfAllowlisted проверяет защиту от SSRF на внутренние адреса.
func TestHTTPGetRejectsInternalIPEvenIfAllowlisted(t *testing.T) {
	// tool формально разрешает localhost IP, но внутренняя сеть должна быть отклонена.
	tool := NewHTTPGetTool(HTTPGetConfig{AllowDomains: []string{"127.0.0.1"}})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"http://127.0.0.1"}`))
	if err == nil {
		t.Fatalf("expected internal IP rejection")
	}
}

// TestIsAllowedDomainSubdomain проверяет, что поддомен считается валидным для базового домена.
func TestIsAllowedDomainSubdomain(t *testing.T) {
	if !isAllowedDomain("api.example.com", []string{"example.com"}) {
		t.Fatalf("expected subdomain to be allowed")
	}
}

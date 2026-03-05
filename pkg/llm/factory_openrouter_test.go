package llm

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// captureDoer фиксирует последний HTTP-запрос для проверки инжектируемых заголовков.
type captureDoer struct {
	lastRequest *http.Request
}

// Do сохраняет запрос и возвращает фиктивный успешный ответ.
func (d *captureDoer) Do(req *http.Request) (*http.Response, error) {
	d.lastRequest = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Header:     make(http.Header),
	}, nil
}

// TestHeaderInjectingHTTPClientAddsHeaders проверяет добавление обязательных заголовков.
func TestHeaderInjectingHTTPClientAddsHeaders(t *testing.T) {
	base := &captureDoer{}
	client := newHeaderInjectingHTTPClient(base, map[string]string{
		"HTTP-Referer": "https://agent-core.local",
		"X-Title":      "agent-core",
	})
	req, err := http.NewRequest(http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	if _, err := client.Do(req); err != nil {
		t.Fatalf("client.Do error: %v", err)
	}
	if base.lastRequest == nil {
		t.Fatalf("base doer did not receive request")
	}
	if got := base.lastRequest.Header.Get("HTTP-Referer"); got != "https://agent-core.local" {
		t.Fatalf("HTTP-Referer = %q", got)
	}
	if got := base.lastRequest.Header.Get("X-Title"); got != "agent-core" {
		t.Fatalf("X-Title = %q", got)
	}
}

// TestHeaderInjectingHTTPClientPreservesExistingHeader проверяет, что уже заданный заголовок не перезаписывается.
func TestHeaderInjectingHTTPClientPreservesExistingHeader(t *testing.T) {
	base := &captureDoer{}
	client := newHeaderInjectingHTTPClient(base, map[string]string{
		"HTTP-Referer": "https://agent-core.local",
	})
	req, err := http.NewRequest(http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("HTTP-Referer", "https://already-set.local")

	if _, err := client.Do(req); err != nil {
		t.Fatalf("client.Do error: %v", err)
	}
	if got := base.lastRequest.Header.Get("HTTP-Referer"); got != "https://already-set.local" {
		t.Fatalf("HTTP-Referer = %q", got)
	}
}

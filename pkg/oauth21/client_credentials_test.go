package oauth21

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientCredentialsTokenSourceCachesToken(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
			t.Fatalf("authorization header = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "client_credentials" {
			t.Fatalf("grant_type = %q", got)
		}
		if got := r.Form.Get("scope"); got != "mcp.read mcp.call" {
			t.Fatalf("scope = %q", got)
		}
		if got := r.Form.Get("audience"); got != "mcp-api" {
			t.Fatalf("audience = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-1",
			"token_type":   "Bearer",
			"expires_in":   600,
		})
	}))
	defer server.Close()

	source, err := NewClientCredentialsTokenSource(ClientCredentialsConfig{
		TokenURL:          server.URL,
		ClientID:          "agent-core",
		ClientSecret:      "secret",
		Audience:          "mcp-api",
		Scopes:            []string{"mcp.read", "mcp.call"},
		AuthMethod:        ClientAuthMethodBasic,
		AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("new token source: %v", err)
	}
	first, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("access token first: %v", err)
	}
	second, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("access token second: %v", err)
	}
	if first != "token-1" || second != "token-1" {
		t.Fatalf("tokens = %q / %q", first, second)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestClientCredentialsTokenSourceUsesDiscoveryAndPostAuth(t *testing.T) {
	var tokenCalls int32
	var server *httptest.Server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuer := server.URL + "/issuer"
		switch r.URL.Path {
		case "/issuer/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         issuer,
				"token_endpoint": issuer + "/token",
			})
		case "/issuer/token":
			atomic.AddInt32(&tokenCalls, 1)
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("client_id"); got != "agent-core" {
				t.Fatalf("client_id = %q", got)
			}
			if got := r.Form.Get("client_secret"); got != "secret" {
				t.Fatalf("client_secret = %q", got)
			}
			if got := r.Form.Get("grant_type"); got != "client_credentials" {
				t.Fatalf("grant_type = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "token-discovery",
				"token_type":   "Bearer",
				"expires_in":   300,
			})
		default:
			http.NotFound(w, r)
		}
	})
	server = httptest.NewServer(handler)
	defer server.Close()

	source, err := NewClientCredentialsTokenSource(ClientCredentialsConfig{
		IssuerURL:         server.URL + "/issuer",
		ClientID:          "agent-core",
		ClientSecret:      "secret",
		AuthMethod:        ClientAuthMethodPost,
		AllowInsecureHTTP: true,
		HTTPTimeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new token source: %v", err)
	}
	token, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("access token: %v", err)
	}
	if token != "token-discovery" {
		t.Fatalf("token = %q", token)
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}
}

func TestValidateEndpointURLRejectsNonHTTPSByDefault(t *testing.T) {
	_, err := ValidateEndpointURL("http://example.com", false, "token_url")
	if err == nil {
		t.Fatalf("expected non-https validation error")
	}
}

func TestValidateEndpointURLAcceptsLocalhostHTTP(t *testing.T) {
	value, err := ValidateEndpointURL("http://localhost:8080", false, "token_url")
	if err != nil {
		t.Fatalf("validate endpoint url: %v", err)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse validated url: %v", err)
	}
	if parsed.Host != "localhost:8080" {
		t.Fatalf("host = %s", parsed.Host)
	}
}

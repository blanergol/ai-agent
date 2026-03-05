package oauth21

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/golang-jwt/jwt/v5"
)

func TestVerifierAcceptsValidToken(t *testing.T) {
	server, issuer, signer := newVerifierTestProvider(t)
	defer server.Close()

	token := signRS256Token(t, signer, "kid-1", jwt.MapClaims{
		"iss":   issuer,
		"aud":   "agent-core",
		"sub":   "user-1",
		"scope": "agent.run mcp.tools.read",
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
		"nbf":   time.Now().Add(-1 * time.Minute).Unix(),
		"iat":   time.Now().Add(-1 * time.Minute).Unix(),
	})
	verifier, err := NewVerifier(VerifierConfig{
		IssuerURL:         issuer,
		Audience:          "agent-core",
		RequiredScopes:    []string{"agent.run"},
		AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	principal, err := verifier.Verify(context.Background(), "Bearer "+token)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if principal.Subject != "user-1" {
		t.Fatalf("subject = %s", principal.Subject)
	}
	if len(principal.Scopes) != 2 {
		t.Fatalf("scopes = %#v", principal.Scopes)
	}
}

func TestVerifierRejectsInsufficientScope(t *testing.T) {
	server, issuer, signer := newVerifierTestProvider(t)
	defer server.Close()

	token := signRS256Token(t, signer, "kid-1", jwt.MapClaims{
		"iss":   issuer,
		"aud":   "agent-core",
		"sub":   "user-1",
		"scope": "agent.read",
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
		"nbf":   time.Now().Add(-1 * time.Minute).Unix(),
		"iat":   time.Now().Add(-1 * time.Minute).Unix(),
	})
	verifier, err := NewVerifier(VerifierConfig{
		IssuerURL:         issuer,
		Audience:          "agent-core",
		RequiredScopes:    []string{"agent.write"},
		AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	_, err = verifier.Verify(context.Background(), "Bearer "+token)
	if apperrors.CodeOf(err) != apperrors.CodeForbidden {
		t.Fatalf("error code = %s, want %s", apperrors.CodeOf(err), apperrors.CodeForbidden)
	}
}

func TestVerifierRejectsExpiredToken(t *testing.T) {
	server, issuer, signer := newVerifierTestProvider(t)
	defer server.Close()

	token := signRS256Token(t, signer, "kid-1", jwt.MapClaims{
		"iss":   issuer,
		"aud":   "agent-core",
		"sub":   "user-1",
		"scope": "agent.run",
		"exp":   time.Now().Add(-1 * time.Minute).Unix(),
		"nbf":   time.Now().Add(-10 * time.Minute).Unix(),
		"iat":   time.Now().Add(-10 * time.Minute).Unix(),
	})
	verifier, err := NewVerifier(VerifierConfig{
		IssuerURL:         issuer,
		Audience:          "agent-core",
		RequiredScopes:    []string{"agent.run"},
		AllowInsecureHTTP: true,
		ClockSkew:         time.Second,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	_, err = verifier.Verify(context.Background(), "Bearer "+token)
	if apperrors.CodeOf(err) != apperrors.CodeAuth {
		t.Fatalf("error code = %s, want %s", apperrors.CodeOf(err), apperrors.CodeAuth)
	}
}

func newVerifierTestProvider(t *testing.T) (*httptest.Server, string, *rsa.PrivateKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	var server *httptest.Server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuer := server.URL + "/issuer"
		switch r.URL.Path {
		case "/issuer/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":   issuer,
				"jwks_uri": issuer + "/jwks",
			})
		case "/issuer/jwks":
			pub := privateKey.Public().(*rsa.PublicKey)
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
				"kty": "RSA",
				"kid": "kid-1",
				"alg": "RS256",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}}})
		default:
			http.NotFound(w, r)
		}
	})
	server = httptest.NewServer(handler)
	return server, server.URL + "/issuer", privateKey
}

func signRS256Token(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return raw
}

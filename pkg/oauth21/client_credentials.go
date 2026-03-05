package oauth21

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/blanergol/agent-core/pkg/apperrors"
)

const (
	// ClientAuthMethodBasic sends client credentials with Authorization: Basic header.
	ClientAuthMethodBasic = "client_secret_basic"
	// ClientAuthMethodPost sends client credentials in form body.
	ClientAuthMethodPost = "client_secret_post"
	// ClientAuthMethodNone sends only client_id for public client scenarios.
	ClientAuthMethodNone = "none"
)

// BearerTokenSource represents access-token provider used by outbound OAuth 2.1 clients.
type BearerTokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// ClientCredentialsConfig configures OAuth 2.1 client credentials grant.
type ClientCredentialsConfig struct {
	IssuerURL         string
	TokenURL          string
	ClientID          string
	ClientSecret      string
	Audience          string
	Scopes            []string
	AuthMethod        string
	ClockSkew         time.Duration
	AllowInsecureHTTP bool
	HTTPTimeout       time.Duration
}

// ClientCredentialsTokenSource fetches and caches OAuth 2.1 access tokens.
type ClientCredentialsTokenSource struct {
	tokenURL     string
	clientID     string
	clientSecret string
	audience     string
	scopes       []string
	authMethod   string
	clockSkew    time.Duration
	httpClient   *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// NewClientCredentialsTokenSource creates OAuth 2.1 token source for client credentials flow.
func NewClientCredentialsTokenSource(cfg ClientCredentialsConfig) (*ClientCredentialsTokenSource, error) {
	httpTimeout := cfg.HTTPTimeout
	if httpTimeout <= 0 {
		httpTimeout = defaultHTTPTimeout
	}
	clockSkew := cfg.ClockSkew
	if clockSkew <= 0 {
		clockSkew = 30 * time.Second
	}
	authMethod := strings.ToLower(strings.TrimSpace(cfg.AuthMethod))
	if authMethod == "" {
		authMethod = ClientAuthMethodBasic
	}
	switch authMethod {
	case ClientAuthMethodBasic, ClientAuthMethodPost, ClientAuthMethodNone:
	default:
		return nil, apperrors.New(apperrors.CodeValidation, "unsupported oauth client auth method", false)
	}
	clientID := strings.TrimSpace(cfg.ClientID)
	if clientID == "" {
		return nil, apperrors.New(apperrors.CodeValidation, "oauth client_id is required", false)
	}
	if authMethod != ClientAuthMethodNone && strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, apperrors.New(apperrors.CodeValidation, "oauth client_secret is required", false)
	}
	issuerURL, err := ValidateEndpointURL(cfg.IssuerURL, cfg.AllowInsecureHTTP, "issuer_url")
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeValidation, "invalid oauth issuer", err, false)
	}
	tokenURL, err := ValidateEndpointURL(cfg.TokenURL, cfg.AllowInsecureHTTP, "token_url")
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeValidation, "invalid oauth token url", err, false)
	}
	client := &http.Client{Timeout: httpTimeout}
	if tokenURL == "" {
		if issuerURL == "" {
			return nil, apperrors.New(apperrors.CodeValidation, "oauth token_url or issuer_url is required", false)
		}
		metadata, err := DiscoverMetadata(context.Background(), issuerURL, client, cfg.AllowInsecureHTTP)
		if err != nil {
			return nil, err
		}
		tokenURL = metadata.TokenEndpoint
	}
	if tokenURL == "" {
		return nil, apperrors.New(apperrors.CodeValidation, "oauth token endpoint is empty", false)
	}
	return &ClientCredentialsTokenSource{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: cfg.ClientSecret,
		audience:     strings.TrimSpace(cfg.Audience),
		scopes:       normalizeUnique(cfg.Scopes),
		authMethod:   authMethod,
		clockSkew:    clockSkew,
		httpClient:   client,
	}, nil
}

// AccessToken returns valid OAuth 2.1 bearer token, refreshing it when expired.
func (s *ClientCredentialsTokenSource) AccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.token != "" && time.Now().Before(s.expiresAt.Add(-s.clockSkew)) {
		token := s.token
		s.mu.Unlock()
		return token, nil
	}
	s.mu.Unlock()

	token, expiry, err := s.fetchToken(ctx)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.token = token
	s.expiresAt = expiry
	s.mu.Unlock()
	return token, nil
}

func (s *ClientCredentialsTokenSource) fetchToken(ctx context.Context) (string, time.Time, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if len(s.scopes) > 0 {
		form.Set("scope", strings.Join(s.scopes, " "))
	}
	if s.audience != "" {
		form.Set("audience", s.audience)
	}
	if s.authMethod == ClientAuthMethodPost || s.authMethod == ClientAuthMethodNone {
		form.Set("client_id", s.clientID)
	}
	if s.authMethod == ClientAuthMethodPost {
		form.Set("client_secret", s.clientSecret)
	}
	encoded := form.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(encoded))
	if err != nil {
		return "", time.Time{}, apperrors.Wrap(apperrors.CodeBadRequest, "build oauth token request", err, false)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if s.authMethod == ClientAuthMethodBasic {
		req.SetBasicAuth(s.clientID, s.clientSecret)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, apperrors.Wrap(apperrors.CodeTransient, "oauth token request failed", err, true)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", time.Time{}, classifyTokenEndpointStatus(resp.StatusCode, body)
	}
	var payload tokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", time.Time{}, apperrors.Wrap(apperrors.CodeTransient, "decode oauth token response", err, true)
	}
	if payload.AccessToken == "" {
		return "", time.Time{}, apperrors.New(apperrors.CodeAuth, "oauth token endpoint returned empty access_token", false)
	}
	if payload.TokenType != "" && !strings.EqualFold(payload.TokenType, "bearer") {
		return "", time.Time{}, apperrors.New(apperrors.CodeAuth, "oauth token endpoint returned non-bearer token", false)
	}
	expiresIn := payload.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 60
	}
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
	return payload.AccessToken, expiresAt, nil
}

func classifyTokenEndpointStatus(status int, body []byte) error {
	message := fmt.Sprintf("oauth token endpoint status: %d", status)
	var payload tokenResponse
	if err := json.Unmarshal(body, &payload); err == nil {
		if strings.TrimSpace(payload.ErrorDescription) != "" {
			message += ": " + strings.TrimSpace(payload.ErrorDescription)
		} else if strings.TrimSpace(payload.Error) != "" {
			message += ": " + strings.TrimSpace(payload.Error)
		}
	}
	switch {
	case status == http.StatusTooManyRequests:
		return apperrors.New(apperrors.CodeRateLimit, message, true)
	case status >= http.StatusInternalServerError:
		return apperrors.New(apperrors.CodeTransient, message, true)
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusBadRequest:
		return apperrors.New(apperrors.CodeAuth, message, false)
	default:
		return apperrors.New(apperrors.CodeBadRequest, message, false)
	}
}

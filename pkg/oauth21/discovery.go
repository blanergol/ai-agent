package oauth21

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/blanergol/agent-core/pkg/apperrors"
)

const defaultHTTPTimeout = 5 * time.Second

// Metadata stores selected OpenID Provider metadata used by OAuth 2.1 flows.
type Metadata struct {
	Issuer        string
	JWKSURI       string
	TokenEndpoint string
}

type openIDConfiguration struct {
	Issuer        string `json:"issuer"`
	JWKSURI       string `json:"jwks_uri"`
	TokenEndpoint string `json:"token_endpoint"`
}

// DiscoverMetadata retrieves OpenID Provider metadata using issuer discovery endpoint.
func DiscoverMetadata(ctx context.Context, issuerURL string, httpClient *http.Client, allowInsecureHTTP bool) (Metadata, error) {
	normalizedIssuer, err := ValidateEndpointURL(issuerURL, allowInsecureHTTP, "issuer_url")
	if err != nil {
		return Metadata{}, apperrors.Wrap(apperrors.CodeValidation, "invalid issuer url", err, false)
	}
	if normalizedIssuer == "" {
		return Metadata{}, apperrors.New(apperrors.CodeValidation, "issuer url is required for discovery", false)
	}
	client := httpClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	discoveryURL := normalizedIssuer + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return Metadata{}, apperrors.Wrap(apperrors.CodeBadRequest, "build discovery request", err, false)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Metadata{}, apperrors.Wrap(apperrors.CodeTransient, "oauth discovery failed", err, true)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Metadata{}, apperrors.New(
			apperrors.CodeAuth,
			fmt.Sprintf("oauth discovery status: %d", resp.StatusCode),
			false,
		)
	}
	var payload openIDConfiguration
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Metadata{}, apperrors.Wrap(apperrors.CodeTransient, "decode oauth discovery payload", err, true)
	}
	if payload.Issuer != "" {
		discovered, err := ValidateEndpointURL(payload.Issuer, allowInsecureHTTP, "discovered_issuer")
		if err != nil {
			return Metadata{}, apperrors.Wrap(apperrors.CodeValidation, "invalid discovered issuer", err, false)
		}
		if !strings.EqualFold(discovered, normalizedIssuer) {
			return Metadata{}, apperrors.New(apperrors.CodeAuth, "discovery issuer mismatch", false)
		}
	}
	jwksURL, err := ValidateEndpointURL(payload.JWKSURI, allowInsecureHTTP, "jwks_uri")
	if err != nil {
		return Metadata{}, apperrors.Wrap(apperrors.CodeValidation, "invalid discovered jwks uri", err, false)
	}
	tokenEndpoint, err := ValidateEndpointURL(payload.TokenEndpoint, allowInsecureHTTP, "token_endpoint")
	if err != nil {
		return Metadata{}, apperrors.Wrap(apperrors.CodeValidation, "invalid discovered token endpoint", err, false)
	}
	return Metadata{
		Issuer:        normalizedIssuer,
		JWKSURI:       jwksURL,
		TokenEndpoint: tokenEndpoint,
	}, nil
}

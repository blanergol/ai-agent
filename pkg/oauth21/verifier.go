package oauth21

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/blanergol/agent-core/pkg/apperrors"
	"github.com/golang-jwt/jwt/v5"
)

var defaultAllowedJWTAlgs = []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"}

// Principal describes authenticated token subject.
type Principal struct {
	Subject string
	Scopes  []string
	Claims  map[string]any
}

// AccessTokenVerifier validates Bearer access tokens.
type AccessTokenVerifier interface {
	Verify(ctx context.Context, authorizationHeader string) (Principal, error)
}

// VerifierConfig configures OAuth 2.1 resource-server access-token validation.
type VerifierConfig struct {
	IssuerURL         string
	JWKSURL           string
	Audience          string
	RequiredScopes    []string
	AllowedAlgs       []string
	ClockSkew         time.Duration
	SubjectClaim      string
	ScopeClaim        string
	AllowInsecureHTTP bool
	HTTPTimeout       time.Duration
}

// Verifier validates JWT access tokens using configured issuer metadata and JWKS.
type Verifier struct {
	issuer         string
	audience       string
	requiredScopes []string
	allowedAlgs    []string
	clockSkew      time.Duration
	subjectClaim   string
	scopeClaim     string
	jwks           *jwksCache
}

// NewVerifier builds OAuth 2.1 access-token verifier.
func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	httpTimeout := cfg.HTTPTimeout
	if httpTimeout <= 0 {
		httpTimeout = defaultHTTPTimeout
	}
	clockSkew := cfg.ClockSkew
	if clockSkew <= 0 {
		clockSkew = 60 * time.Second
	}
	subjectClaim := strings.TrimSpace(cfg.SubjectClaim)
	if subjectClaim == "" {
		subjectClaim = "sub"
	}
	scopeClaim := strings.TrimSpace(cfg.ScopeClaim)
	if scopeClaim == "" {
		scopeClaim = "scope"
	}
	allowedAlgs := normalizeUnique(cfg.AllowedAlgs)
	if len(allowedAlgs) == 0 {
		allowedAlgs = append([]string(nil), defaultAllowedJWTAlgs...)
	}
	issuer, err := ValidateEndpointURL(cfg.IssuerURL, cfg.AllowInsecureHTTP, "issuer_url")
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeValidation, "invalid oauth issuer", err, false)
	}
	jwksURL, err := ValidateEndpointURL(cfg.JWKSURL, cfg.AllowInsecureHTTP, "jwks_url")
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeValidation, "invalid oauth jwks url", err, false)
	}
	if issuer == "" && jwksURL == "" {
		return nil, apperrors.New(apperrors.CodeValidation, "oauth verifier requires issuer_url or jwks_url", false)
	}
	client := &http.Client{Timeout: httpTimeout}
	if jwksURL == "" {
		metadata, err := DiscoverMetadata(context.Background(), issuer, client, cfg.AllowInsecureHTTP)
		if err != nil {
			return nil, err
		}
		if metadata.Issuer != "" {
			issuer = metadata.Issuer
		}
		jwksURL = metadata.JWKSURI
	}
	if jwksURL == "" {
		return nil, apperrors.New(apperrors.CodeValidation, "oauth verifier jwks url is empty", false)
	}
	return &Verifier{
		issuer:         issuer,
		audience:       strings.TrimSpace(cfg.Audience),
		requiredScopes: normalizeUnique(cfg.RequiredScopes),
		allowedAlgs:    allowedAlgs,
		clockSkew:      clockSkew,
		subjectClaim:   subjectClaim,
		scopeClaim:     scopeClaim,
		jwks:           newJWKSCache(jwksURL, client),
	}, nil
}

// Verify validates Bearer token and returns token principal.
func (v *Verifier) Verify(ctx context.Context, authorizationHeader string) (Principal, error) {
	tokenValue, err := extractBearerToken(authorizationHeader)
	if err != nil {
		return Principal{}, err
	}
	claims := jwt.MapClaims{}
	parseOptions := []jwt.ParserOption{
		jwt.WithValidMethods(v.allowedAlgs),
		jwt.WithLeeway(v.clockSkew),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
	}
	if strings.TrimSpace(v.issuer) != "" {
		parseOptions = append(parseOptions, jwt.WithIssuer(v.issuer))
	}
	if strings.TrimSpace(v.audience) != "" {
		parseOptions = append(parseOptions, jwt.WithAudience(v.audience))
	}
	parser := jwt.NewParser(parseOptions...)
	token, err := parser.ParseWithClaims(tokenValue, claims, func(parsed *jwt.Token) (any, error) {
		kid, _ := parsed.Header["kid"].(string)
		return v.jwks.key(ctx, strings.TrimSpace(kid), parsed.Method.Alg())
	})
	if err != nil {
		return Principal{}, mapTokenParseError(err)
	}
	if token == nil || !token.Valid {
		return Principal{}, apperrors.New(apperrors.CodeAuth, "invalid access token", false)
	}
	subject := strings.TrimSpace(claimToString(claims[v.subjectClaim]))
	if subject == "" {
		return Principal{}, apperrors.New(apperrors.CodeAuth, "access token subject is missing", false)
	}
	scopes := parseScopes(claims[v.scopeClaim])
	if !containsAll(scopes, v.requiredScopes) {
		return Principal{}, apperrors.New(apperrors.CodeForbidden, "insufficient token scope", false)
	}
	return Principal{
		Subject: subject,
		Scopes:  scopes,
		Claims:  cloneClaims(claims),
	}, nil
}

func extractBearerToken(headerValue string) (string, error) {
	raw := strings.TrimSpace(headerValue)
	if raw == "" {
		return "", apperrors.New(apperrors.CodeAuth, "missing bearer token", false)
	}
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "bearer") {
		return "", apperrors.New(apperrors.CodeAuth, "invalid bearer token format", false)
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", apperrors.New(apperrors.CodeAuth, "missing bearer token", false)
	}
	return token, nil
}

func mapTokenParseError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired),
		errors.Is(err, jwt.ErrTokenNotValidYet),
		errors.Is(err, jwt.ErrTokenMalformed),
		errors.Is(err, jwt.ErrTokenSignatureInvalid),
		errors.Is(err, jwt.ErrTokenUnverifiable),
		errors.Is(err, jwt.ErrTokenInvalidClaims),
		errors.Is(err, jwt.ErrTokenInvalidIssuer),
		errors.Is(err, jwt.ErrTokenInvalidAudience):
		return apperrors.Wrap(apperrors.CodeAuth, "invalid access token", err, false)
	default:
		if apperrors.CodeOf(err) != "" {
			return err
		}
		return apperrors.Wrap(apperrors.CodeAuth, "access token verification failed", err, false)
	}
}

func parseScopes(value any) []string {
	switch typed := value.(type) {
	case string:
		return normalizeUnique(strings.Fields(strings.TrimSpace(typed)))
	case []string:
		return normalizeUnique(typed)
	case []any:
		scopes := make([]string, 0, len(typed))
		for _, item := range typed {
			scopes = append(scopes, claimToString(item))
		}
		return normalizeUnique(scopes)
	default:
		return nil
	}
}

func containsAll(have []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	if len(have) == 0 {
		return false
	}
	lookup := make(map[string]struct{}, len(have))
	for _, item := range have {
		if item == "" {
			continue
		}
		lookup[item] = struct{}{}
	}
	for _, item := range required {
		if _, ok := lookup[item]; !ok {
			return false
		}
	}
	return true
}

func normalizeUnique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneClaims(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func claimToString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

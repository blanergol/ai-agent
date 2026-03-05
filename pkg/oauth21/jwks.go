package oauth21

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/blanergol/agent-core/pkg/apperrors"
)

const defaultJWKSCacheTTL = 5 * time.Minute

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KeyType string `json:"kty"`
	KeyID   string `json:"kid"`
	Use     string `json:"use"`
	Alg     string `json:"alg"`
	N       string `json:"n"`
	E       string `json:"e"`
	Crv     string `json:"crv"`
	X       string `json:"x"`
	Y       string `json:"y"`
}

type jwksKey struct {
	id  string
	alg string
	key any
}

type jwksCache struct {
	url    string
	client *http.Client

	mu        sync.Mutex
	keys      []jwksKey
	expiresAt time.Time
}

func newJWKSCache(url string, client *http.Client) *jwksCache {
	return &jwksCache{url: url, client: client}
}

func (c *jwksCache) key(ctx context.Context, kid string, alg string) (any, error) {
	if key, ok := c.lookup(kid, alg, time.Now()); ok {
		return key, nil
	}
	if err := c.refresh(ctx); err != nil {
		return nil, err
	}
	if key, ok := c.lookup(kid, alg, time.Now()); ok {
		return key, nil
	}
	if strings.TrimSpace(kid) != "" {
		return nil, apperrors.New(apperrors.CodeAuth, "jwks key id not found", false)
	}
	return nil, apperrors.New(apperrors.CodeAuth, "jwks key not found", false)
}

func (c *jwksCache) lookup(kid string, alg string, now time.Time) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.keys) == 0 || now.After(c.expiresAt) {
		return nil, false
	}
	if strings.TrimSpace(kid) != "" {
		for _, entry := range c.keys {
			if entry.id != kid {
				continue
			}
			if !isJWTAlgCompatible(entry, alg) {
				return nil, false
			}
			return entry.key, true
		}
		return nil, false
	}
	var matched []jwksKey
	for _, entry := range c.keys {
		if !isJWTAlgCompatible(entry, alg) {
			continue
		}
		matched = append(matched, entry)
	}
	if len(matched) == 1 {
		return matched[0].key, true
	}
	if len(matched) > 1 {
		return nil, false
	}
	if len(c.keys) == 1 {
		return c.keys[0].key, true
	}
	return nil, false
}

func (c *jwksCache) refresh(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.keys) > 0 && time.Now().Before(c.expiresAt) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return apperrors.Wrap(apperrors.CodeBadRequest, "build jwks request", err, false)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return apperrors.Wrap(apperrors.CodeTransient, "fetch jwks", err, true)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return apperrors.New(apperrors.CodeAuth, fmt.Sprintf("jwks endpoint status: %d", resp.StatusCode), false)
	}
	var payload jwksDocument
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return apperrors.Wrap(apperrors.CodeTransient, "decode jwks payload", err, true)
	}
	converted := make([]jwksKey, 0, len(payload.Keys))
	for _, item := range payload.Keys {
		if strings.TrimSpace(item.Use) != "" && !strings.EqualFold(item.Use, "sig") {
			continue
		}
		key, err := parseJWK(item)
		if err != nil {
			continue
		}
		converted = append(converted, jwksKey{
			id:  strings.TrimSpace(item.KeyID),
			alg: strings.TrimSpace(item.Alg),
			key: key,
		})
	}
	if len(converted) == 0 {
		return apperrors.New(apperrors.CodeAuth, "jwks does not contain usable signing keys", false)
	}
	c.keys = converted
	c.expiresAt = time.Now().Add(parseCacheTTL(resp.Header.Get("Cache-Control")))
	return nil
}

func parseJWK(item jwk) (any, error) {
	switch strings.ToUpper(strings.TrimSpace(item.KeyType)) {
	case "RSA":
		modulus, err := decodeBase64URLInt(item.N)
		if err != nil {
			return nil, err
		}
		exponent, err := decodeBase64URLInt(item.E)
		if err != nil {
			return nil, err
		}
		e := int(exponent.Int64())
		if e <= 0 {
			return nil, fmt.Errorf("invalid rsa exponent")
		}
		return &rsa.PublicKey{N: modulus, E: e}, nil
	case "EC":
		curve, err := resolveCurve(item.Crv)
		if err != nil {
			return nil, err
		}
		x, err := decodeBase64URLInt(item.X)
		if err != nil {
			return nil, err
		}
		y, err := decodeBase64URLInt(item.Y)
		if err != nil {
			return nil, err
		}
		if !curve.IsOnCurve(x, y) {
			return nil, fmt.Errorf("ec key coordinates are not on curve")
		}
		return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
	case "OKP":
		if !strings.EqualFold(strings.TrimSpace(item.Crv), "Ed25519") {
			return nil, fmt.Errorf("unsupported okp curve")
		}
		x, err := base64.RawURLEncoding.DecodeString(item.X)
		if err != nil {
			return nil, fmt.Errorf("decode okp x: %w", err)
		}
		if len(x) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid ed25519 key length")
		}
		return ed25519.PublicKey(x), nil
	default:
		return nil, fmt.Errorf("unsupported jwk kty: %s", item.KeyType)
	}
}

func decodeBase64URLInt(value string) (*big.Int, error) {
	buf, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode base64url int: %w", err)
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("empty integer value")
	}
	out := new(big.Int).SetBytes(buf)
	return out, nil
}

func resolveCurve(crv string) (elliptic.Curve, error) {
	switch strings.TrimSpace(crv) {
	case "P-256":
		return elliptic.P256(), nil
	case "P-384":
		return elliptic.P384(), nil
	case "P-521":
		return elliptic.P521(), nil
	default:
		return nil, fmt.Errorf("unsupported ec curve: %s", crv)
	}
}

func isJWTAlgCompatible(entry jwksKey, requestedAlg string) bool {
	alg := strings.TrimSpace(requestedAlg)
	if alg == "" {
		return true
	}
	if strings.TrimSpace(entry.alg) != "" && !strings.EqualFold(entry.alg, alg) {
		return false
	}
	switch {
	case strings.HasPrefix(strings.ToUpper(alg), "RS"):
		_, ok := entry.key.(*rsa.PublicKey)
		return ok
	case strings.HasPrefix(strings.ToUpper(alg), "ES"):
		_, ok := entry.key.(*ecdsa.PublicKey)
		return ok
	case strings.EqualFold(alg, "EdDSA"):
		_, ok := entry.key.(ed25519.PublicKey)
		return ok
	default:
		return false
	}
}

func parseCacheTTL(cacheControl string) time.Duration {
	value := strings.TrimSpace(cacheControl)
	if value == "" {
		return defaultJWKSCacheTTL
	}
	parts := strings.Split(value, ",")
	for _, part := range parts {
		token := strings.TrimSpace(strings.ToLower(part))
		if !strings.HasPrefix(token, "max-age=") {
			continue
		}
		secondsRaw := strings.TrimSpace(strings.TrimPrefix(token, "max-age="))
		seconds, err := time.ParseDuration(secondsRaw + "s")
		if err != nil || seconds <= 0 {
			continue
		}
		return seconds
	}
	return defaultJWKSCacheTTL
}

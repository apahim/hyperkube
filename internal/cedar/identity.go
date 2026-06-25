package cedar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	googleJWKSURL    = "https://www.googleapis.com/oauth2/v3/certs"
	googleIssuerV1   = "accounts.google.com"
	googleIssuerV2   = "https://accounts.google.com"
	jwksCacheTTL     = 1 * time.Hour
	jwksFetchTimeout = 10 * time.Second
)

type Identity struct {
	UserID string
	Email  string
}

type googleClaims struct {
	jwt.Claims
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

type JWTValidator struct {
	jwksURL    string
	httpClient *http.Client

	mu         sync.RWMutex
	cachedJWKS *jose.JSONWebKeySet
	cachedAt   time.Time
}

func NewJWTValidator() *JWTValidator {
	return &JWTValidator{
		jwksURL:    googleJWKSURL,
		httpClient: &http.Client{Timeout: jwksFetchTimeout},
	}
}

func NewJWTValidatorWithURL(jwksURL string) *JWTValidator {
	return &JWTValidator{
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: jwksFetchTimeout},
	}
}

func (v *JWTValidator) ValidateRequest(r *http.Request) (*Identity, error) {
	token, err := extractBearerToken(r)
	if err != nil {
		return nil, err
	}
	return v.validateToken(r.Context(), token)
}

func extractBearerToken(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", fmt.Errorf("missing Authorization header")
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", fmt.Errorf("authorization header must use Bearer scheme")
	}
	return strings.TrimPrefix(auth, "Bearer "), nil
}

func (v *JWTValidator) validateToken(ctx context.Context, rawToken string) (*Identity, error) {
	tok, err := jwt.ParseSigned(rawToken, []jose.SignatureAlgorithm{jose.RS256, jose.ES256})
	if err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}

	jwks, err := v.getJWKS(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching JWKS: %w", err)
	}

	if len(tok.Headers) == 0 {
		return nil, fmt.Errorf("token has no headers")
	}
	kid := tok.Headers[0].KeyID
	keys := jwks.Key(kid)
	if len(keys) == 0 {
		jwks, err = v.fetchJWKS(ctx)
		if err != nil {
			return nil, fmt.Errorf("refreshing JWKS: %w", err)
		}
		keys = jwks.Key(kid)
		if len(keys) == 0 {
			return nil, fmt.Errorf("key %q not found in JWKS", kid)
		}
	}

	var claims googleClaims
	if err := tok.Claims(keys[0].Key, &claims); err != nil {
		return nil, fmt.Errorf("verifying token signature: %w", err)
	}

	if claims.Issuer != googleIssuerV1 && claims.Issuer != googleIssuerV2 {
		return nil, fmt.Errorf("unexpected issuer %q", claims.Issuer)
	}

	if claims.Expiry != nil && time.Now().After(claims.Expiry.Time()) {
		return nil, fmt.Errorf("token expired")
	}

	userID := claims.Subject
	if userID == "" {
		return nil, fmt.Errorf("token missing subject claim")
	}

	return &Identity{
		UserID: userID,
		Email:  claims.Email,
	}, nil
}

func (v *JWTValidator) getJWKS(ctx context.Context) (*jose.JSONWebKeySet, error) {
	v.mu.RLock()
	if v.cachedJWKS != nil && time.Since(v.cachedAt) < jwksCacheTTL {
		jwks := v.cachedJWKS
		v.mu.RUnlock()
		return jwks, nil
	}
	v.mu.RUnlock()
	return v.fetchJWKS(ctx)
}

func (v *JWTValidator) fetchJWKS(ctx context.Context) (*jose.JSONWebKeySet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.cachedJWKS != nil && time.Since(v.cachedAt) < 5*time.Second {
		return v.cachedJWKS, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("JWKS endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var jwks jose.JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("decoding JWKS: %w", err)
	}

	v.cachedJWKS = &jwks
	v.cachedAt = time.Now()
	return &jwks, nil
}

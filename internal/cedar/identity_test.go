package cedar

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type testJWKS struct {
	key    *rsa.PrivateKey
	kid    string
	server *httptest.Server
}

func newTestJWKS(t *testing.T) *testJWKS {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key-1"

	jwks := jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: kid, Algorithm: string(jose.RS256), Use: "sig"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)

	return &testJWKS{key: key, kid: kid, server: srv}
}

func (tj *testJWKS) signToken(t *testing.T, claims googleClaims) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: tj.key},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), tj.kid),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestValidateRequest_ValidToken(t *testing.T) {
	tj := newTestJWKS(t)
	v := NewJWTValidatorWithURL(tj.server.URL)

	claims := googleClaims{
		Claims: jwt.Claims{
			Issuer:  "accounts.google.com",
			Subject: "user-123",
			Expiry:  jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
		Email: "alice@example.com",
	}
	token := tj.signToken(t, claims)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := v.ValidateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != "user-123" {
		t.Errorf("userID: got %q, want %q", identity.UserID, "user-123")
	}
	if identity.Email != "alice@example.com" {
		t.Errorf("email: got %q, want %q", identity.Email, "alice@example.com")
	}
}

func TestValidateRequest_MissingHeader(t *testing.T) {
	v := NewJWTValidator()
	req := httptest.NewRequest("GET", "/test", nil)

	_, err := v.ValidateRequest(req)
	if err == nil {
		t.Fatal("expected error for missing Authorization header")
	}
}

func TestValidateRequest_InvalidScheme(t *testing.T) {
	v := NewJWTValidator()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	_, err := v.ValidateRequest(req)
	if err == nil {
		t.Fatal("expected error for non-Bearer scheme")
	}
}

func TestValidateRequest_ExpiredToken(t *testing.T) {
	tj := newTestJWKS(t)
	v := NewJWTValidatorWithURL(tj.server.URL)

	claims := googleClaims{
		Claims: jwt.Claims{
			Issuer:  "accounts.google.com",
			Subject: "user-123",
			Expiry:  jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		},
		Email: "alice@example.com",
	}
	token := tj.signToken(t, claims)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	_, err := v.ValidateRequest(req)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateRequest_WrongIssuer(t *testing.T) {
	tj := newTestJWKS(t)
	v := NewJWTValidatorWithURL(tj.server.URL)

	claims := googleClaims{
		Claims: jwt.Claims{
			Issuer:  "https://evil.example.com",
			Subject: "user-123",
			Expiry:  jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	}
	token := tj.signToken(t, claims)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	_, err := v.ValidateRequest(req)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestValidateRequest_GoogleIssuerV2(t *testing.T) {
	tj := newTestJWKS(t)
	v := NewJWTValidatorWithURL(tj.server.URL)

	claims := googleClaims{
		Claims: jwt.Claims{
			Issuer:  "https://accounts.google.com",
			Subject: "user-456",
			Expiry:  jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
		Email: "bob@example.com",
	}
	token := tj.signToken(t, claims)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := v.ValidateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != "user-456" {
		t.Errorf("userID: got %q, want %q", identity.UserID, "user-456")
	}
}

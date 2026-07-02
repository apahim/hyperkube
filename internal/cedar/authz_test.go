package cedar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
)

func newAuthzTestSetup(t *testing.T) (*AuthzMiddleware, *testJWKS) {
	t.Helper()
	store := newTestStore(t)

	tj := newTestJWKS(t)
	validator := NewJWTValidatorWithURL(tj.server.URL)

	mw := NewAuthzMiddleware(store, validator, nil, nil)
	return mw, tj
}

func makeAuthRequest(t *testing.T, tj *testJWKS, method, path, userID string) *http.Request {
	t.Helper()
	claims := googleClaims{
		Claims: jwt.Claims{
			Issuer:  "accounts.google.com",
			Subject: userID,
			Expiry:  jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
		Email: userID + "@example.com",
	}
	token := tj.signToken(t, claims)
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestAuthzMiddleware_NonAPIPathBypasses(t *testing.T) {
	mw, _ := newAuthzTestSetup(t)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for non-API path, got %d", rec.Code)
	}
}

func TestAuthzMiddleware_MissingAuth(t *testing.T) {
	mw, _ := newAuthzTestSetup(t)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1alpha1/namespaces/my-project/managedhostedclusters", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing auth, got %d", rec.Code)
	}
}

func TestAuthzMiddleware_NoPoliciesDenies(t *testing.T) {
	mw, tj := newAuthzTestSetup(t)
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := makeAuthRequest(t, tj, "GET", "/v1alpha1/namespaces/my-project/managedhostedclusters", "alice")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 with no policies, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthzMiddleware_AllowWithPolicy(t *testing.T) {
	mw, tj := newAuthzTestSetup(t)

	_, err := mw.store.CreateAttachment(context.Background(), "my-project", "cluster-viewer", "alice")
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := makeAuthRequest(t, tj, "GET", "/v1alpha1/namespaces/my-project/managedhostedclusters", "alice")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with matching policy, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthzMiddleware_DenyWrongAction(t *testing.T) {
	mw, tj := newAuthzTestSetup(t)

	_, err := mw.store.CreateAttachment(context.Background(), "my-project", "cluster-viewer", "alice")
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := makeAuthRequest(t, tj, "POST", "/v1alpha1/namespaces/my-project/managedhostedclusters", "alice")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for create with viewer-only policy, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthzMiddleware_DenyWrongUser(t *testing.T) {
	mw, tj := newAuthzTestSetup(t)

	_, err := mw.store.CreateAttachment(context.Background(), "my-project", "cluster-viewer", "alice")
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := makeAuthRequest(t, tj, "GET", "/v1alpha1/namespaces/my-project/managedhostedclusters", "bob")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for wrong user, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthzMiddleware_ServiceAdminAllowsDelete(t *testing.T) {
	mw, tj := newAuthzTestSetup(t)

	_, err := mw.store.CreateAttachment(context.Background(), "my-project", "service-admin", "alice")
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := makeAuthRequest(t, tj, "DELETE", "/v1alpha1/namespaces/my-project/managedhostedclusters/cluster-1", "alice")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with service-admin policy, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthzMiddleware_ClusterAdminCanCreate(t *testing.T) {
	mw, tj := newAuthzTestSetup(t)

	_, err := mw.store.CreateAttachment(context.Background(), "my-project", "cluster-admin", "alice")
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := makeAuthRequest(t, tj, "POST", "/v1alpha1/namespaces/my-project/managedhostedclusters", "alice")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with cluster-admin policy for create, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthzMiddleware_DeveloperCannotCreate(t *testing.T) {
	mw, tj := newAuthzTestSetup(t)

	_, err := mw.store.CreateAttachment(context.Background(), "my-project", "developer", "alice")
	if err != nil {
		t.Fatal(err)
	}

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := makeAuthRequest(t, tj, "POST", "/v1alpha1/namespaces/my-project/managedhostedclusters", "alice")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for developer trying to create, got %d: %s", rec.Code, rec.Body.String())
	}
}

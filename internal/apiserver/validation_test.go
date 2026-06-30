package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gcp-hcp/gcp-hcp-backend/api/openapi"
)

func newTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestValidationMiddleware_ValidPost(t *testing.T) {
	mw, err := NewValidationMiddleware(openapi.Spec)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"metadata":{"name":"test"},"spec":{"clusterID":"c1","versionStreamRef":{"name":"stable"},"hostedCluster":{"release":{"image":"img:v1"},"infraID":"c1","platform":{"type":"None"}}}}`
	req := httptest.NewRequest("POST", "/v1alpha1/namespaces/default/managedhostedclusters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mw.Wrap(newTestHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestValidationMiddleware_MissingRequiredField(t *testing.T) {
	mw, err := NewValidationMiddleware(openapi.Spec)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"metadata":{"name":"test"},"spec":{"versionStreamRef":{"name":"stable"},"hostedCluster":{"release":{"image":"img:v1"},"infraID":"c1","platform":{"type":"None"}}}}`
	req := httptest.NewRequest("POST", "/v1alpha1/namespaces/default/managedhostedclusters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mw.Wrap(newTestHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing clusterID, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestValidationMiddleware_GetPassesThrough(t *testing.T) {
	mw, err := NewValidationMiddleware(openapi.Spec)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/v1alpha1/namespaces/default/managedhostedclusters", nil)
	rec := httptest.NewRecorder()

	mw.Wrap(newTestHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestValidationMiddleware_NonAPIPathBypasses(t *testing.T) {
	mw, err := NewValidationMiddleware(openapi.Spec)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()

	mw.Wrap(newTestHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestValidationMiddleware_MissingSpec(t *testing.T) {
	mw, err := NewValidationMiddleware(openapi.Spec)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"metadata":{"name":"test"}}`
	req := httptest.NewRequest("POST", "/v1alpha1/namespaces/default/managedhostedclusters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mw.Wrap(newTestHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing spec, got %d: %s", rec.Code, rec.Body.String())
	}
}

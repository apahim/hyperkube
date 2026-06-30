package cedar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hcpv1alpha1 "github.com/gcp-hcp/gcp-hcp-backend/api/v1alpha1"
)

func newTestAuthzHandler(t *testing.T) *AuthzHandler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := hcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	store, err := NewStore(fakeClient, "cedar-policies", "")
	if err != nil {
		t.Fatal(err)
	}
	return &AuthzHandler{Store: store}
}

func TestListTemplates(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authz/templates", h.ListTemplates)

	req := httptest.NewRequest("GET", "/authz/templates", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var templates []Template
	if err := json.NewDecoder(rec.Body).Decode(&templates); err != nil {
		t.Fatal(err)
	}
	if len(templates) != 4 {
		t.Errorf("expected 4 templates, got %d", len(templates))
	}
}

func TestGetTemplate(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authz/templates/{name}", h.GetTemplate)

	req := httptest.NewRequest("GET", "/authz/templates/cluster-viewer", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tmpl Template
	if err := json.NewDecoder(rec.Body).Decode(&tmpl); err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "cluster-viewer" {
		t.Errorf("expected name 'cluster-viewer', got %q", tmpl.Name)
	}
}

func TestGetTemplate_NotFound(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authz/templates/{name}", h.GetTemplate)

	req := httptest.NewRequest("GET", "/authz/templates/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestCreateAndListAttachments(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /authz/namespaces/{namespace}/attachments", h.CreateAttachment)
	mux.HandleFunc("GET /authz/namespaces/{namespace}/attachments", h.ListAttachments)

	body := `{"template_name":"cluster-viewer","user_id":"alice"}`
	req := httptest.NewRequest("POST", "/authz/namespaces/my-project/attachments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest("GET", "/authz/namespaces/my-project/attachments", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	var attachments []Attachment
	if err := json.NewDecoder(rec.Body).Decode(&attachments); err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 {
		t.Errorf("expected 1 attachment, got %d", len(attachments))
	}
}

func TestCreateAttachment_MissingFields(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /authz/namespaces/{namespace}/attachments", h.CreateAttachment)

	body := `{"template_name":"cluster-viewer"}`
	req := httptest.NewRequest("POST", "/authz/namespaces/my-project/attachments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteAttachment(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /authz/namespaces/{namespace}/attachments", h.CreateAttachment)
	mux.HandleFunc("DELETE /authz/namespaces/{namespace}/attachments/{id}", h.DeleteAttachment)
	mux.HandleFunc("GET /authz/namespaces/{namespace}/attachments", h.ListAttachments)

	body := `{"template_name":"cluster-viewer","user_id":"alice"}`
	req := httptest.NewRequest("POST", "/authz/namespaces/my-project/attachments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var att Attachment
	if err := json.NewDecoder(rec.Body).Decode(&att); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest("DELETE", "/authz/namespaces/my-project/attachments/"+att.ID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest("GET", "/authz/namespaces/my-project/attachments", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var remaining []Attachment
	if err := json.NewDecoder(rec.Body).Decode(&remaining); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 attachments after delete, got %d", len(remaining))
	}
}

func TestListAttachments_Empty(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authz/namespaces/{namespace}/attachments", h.ListAttachments)

	req := httptest.NewRequest("GET", "/authz/namespaces/no-project/attachments", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var attachments []Attachment
	if err := json.NewDecoder(rec.Body).Decode(&attachments); err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(attachments))
	}
}

func TestListRolesHandler(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authz/namespaces/{namespace}/roles", h.ListRoles)

	req := httptest.NewRequest("GET", "/authz/namespaces/my-project/roles", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var roles []Role
	if err := json.NewDecoder(rec.Body).Decode(&roles); err != nil {
		t.Fatal(err)
	}
	if len(roles) != 4 {
		t.Errorf("expected 4 predefined roles, got %d", len(roles))
	}
}

func TestGetRoleHandler(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authz/namespaces/{namespace}/roles/{name}", h.GetRole)

	req := httptest.NewRequest("GET", "/authz/namespaces/my-project/roles/cluster-admin", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var role Role
	if err := json.NewDecoder(rec.Body).Decode(&role); err != nil {
		t.Fatal(err)
	}
	if role.Name != "cluster-admin" {
		t.Errorf("expected name 'cluster-admin', got %q", role.Name)
	}
	if !role.Predefined {
		t.Error("expected predefined to be true")
	}
}

func TestGetRoleHandler_NotFound(t *testing.T) {
	h := newTestAuthzHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authz/namespaces/{namespace}/roles/{name}", h.GetRole)

	req := httptest.NewRequest("GET", "/authz/namespaces/my-project/roles/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

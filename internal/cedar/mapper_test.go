package cedar

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMapRequest(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		wantAction  string
		wantResType string
		wantResID   string
		wantProject string
		wantNil     bool
	}{
		{"list clusters", "GET", "/v1alpha1/namespaces/my-project/managedhostedclusters", "ReadManagedHostedCluster", "Project", "my-project", "my-project", false},
		{"create cluster", "POST", "/v1alpha1/namespaces/my-project/managedhostedclusters", "WriteManagedHostedCluster", "Project", "my-project", "my-project", false},
		{"get cluster", "GET", "/v1alpha1/namespaces/my-project/managedhostedclusters/cluster-1", "ReadManagedHostedCluster", "ManagedHostedCluster", "cluster-1", "my-project", false},
		{"update cluster", "PUT", "/v1alpha1/namespaces/my-project/managedhostedclusters/cluster-1", "WriteManagedHostedCluster", "ManagedHostedCluster", "cluster-1", "my-project", false},
		{"delete cluster", "DELETE", "/v1alpha1/namespaces/my-project/managedhostedclusters/cluster-1", "WriteManagedHostedCluster", "ManagedHostedCluster", "cluster-1", "my-project", false},
		{"healthz bypassed", "GET", "/healthz", "", "", "", "", true},
		{"openapi bypassed", "GET", "/openapi.yaml", "", "", "", "", true},
		{"authz bypassed", "GET", "/authz/templates", "", "", "", "", true},
		{"unknown path bypassed", "GET", "/unknown", "", "", "", "", true},
		{"wrong method on collection", "DELETE", "/v1alpha1/namespaces/ns/managedhostedclusters", "", "", "", "", true},
		{"wrong method on resource", "POST", "/v1alpha1/namespaces/ns/managedhostedclusters/c1", "", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			got := MapRequest(req)

			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected mapping, got nil")
			}
			if got.Action != tt.wantAction {
				t.Errorf("action: got %q, want %q", got.Action, tt.wantAction)
			}
			if got.ResourceType != tt.wantResType {
				t.Errorf("resourceType: got %q, want %q", got.ResourceType, tt.wantResType)
			}
			if got.ResourceID != tt.wantResID {
				t.Errorf("resourceID: got %q, want %q", got.ResourceID, tt.wantResID)
			}
			if got.ProjectID != tt.wantProject {
				t.Errorf("projectID: got %q, want %q", got.ProjectID, tt.wantProject)
			}
		})
	}
}

func BenchmarkMapRequest(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/v1alpha1/namespaces/ns/managedhostedclusters/c1", nil)
	b.ResetTimer()
	for b.Loop() {
		MapRequest(req)
	}
}

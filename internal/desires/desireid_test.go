package desires

import (
	"testing"
)

func TestNewDocumentID(t *testing.T) {
	tests := []struct {
		name      string
		taskKey   string
		group     string
		version   string
		resource  string
		namespace string
		objName   string
	}{
		{
			name:      "ManagedHostedCluster",
			taskKey:   TaskKey,
			group:     "hcp.gcp.hypershift.openshift.com",
			version:   "v1alpha1",
			resource:  "managedhostedclusters",
			namespace: "my-project",
			objName:   "my-cluster",
		},
		{
			name:      "cluster-scoped resource",
			taskKey:   TaskKey,
			group:     "hcp.gcp.hypershift.openshift.com",
			version:   "v1alpha1",
			resource:  "versionstreams",
			namespace: "",
			objName:   "stable-4-16",
		},
		{
			name:      "core group resource",
			taskKey:   "desirectl",
			group:     "",
			version:   "v1",
			resource:  "configmaps",
			namespace: "default",
			objName:   "test-cm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := NewDocumentID(tt.taskKey, tt.group, tt.version, tt.resource, tt.namespace, tt.objName)
			if id == "" {
				t.Fatal("expected non-empty document ID")
			}
			if len(id) != 36 {
				t.Fatalf("expected UUID format (36 chars), got %d: %s", len(id), id)
			}

			id2 := NewDocumentID(tt.taskKey, tt.group, tt.version, tt.resource, tt.namespace, tt.objName)
			if id != id2 {
				t.Fatalf("determinism broken: %s != %s", id, id2)
			}
		})
	}
}

func TestNewDocumentID_DifferentTaskKeyProducesDifferentID(t *testing.T) {
	id1 := NewDocumentID("task-a", "apps", "v1", "deployments", "default", "nginx")
	id2 := NewDocumentID("task-b", "apps", "v1", "deployments", "default", "nginx")
	if id1 == id2 {
		t.Fatalf("different taskKeys should produce different IDs: %s == %s", id1, id2)
	}
}

func TestNewDocumentID_MatchesKubeApplier(t *testing.T) {
	// Verify our output matches the kube-applier's desireid.NewDocumentID
	// by computing a known input and comparing against the expected UUID.
	// Both implementations use UUID v5 with the same namespace UUID and
	// the same input format "taskKey/group/version/resource/namespace/name".
	id := NewDocumentID("desirectl", "", "v1", "configmaps", "default", "test")
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
	// The exact value is deterministic — if this test ever fails after a
	// code change, it means the ID generation has drifted from the contract.
	expected := "3ce2d08b-706f-53f6-a255-f005e65a799c"
	if id != expected {
		t.Fatalf("ID drift from kube-applier contract: got %s, want %s", id, expected)
	}
}

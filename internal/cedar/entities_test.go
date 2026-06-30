package cedar

import (
	"testing"

	cedarlib "github.com/cedar-policy/cedar-go"
)

func TestBuildEntityMap_ProjectLevel(t *testing.T) {
	em, err := BuildEntityMap("alice", "my-project", "alice@example.com", "Project", "my-project", nil)
	if err != nil {
		t.Fatal(err)
	}

	userUID := cedarlib.NewEntityUID("HCP::User", "alice")
	user, ok := em.Get(userUID)
	if !ok {
		t.Fatal("expected user entity")
	}
	emailVal, ok := user.Attributes.Get("email")
	if !ok {
		t.Fatal("expected email attribute")
	}
	if emailVal != cedarlib.String("alice@example.com") {
		t.Errorf("email: got %v, want alice@example.com", emailVal)
	}

	projectUID := cedarlib.NewEntityUID("HCP::Project", "my-project")
	_, ok = em.Get(projectUID)
	if !ok {
		t.Fatal("expected project entity")
	}

	clusterUID := cedarlib.NewEntityUID("HCP::ManagedHostedCluster", "anything")
	_, ok = em.Get(clusterUID)
	if ok {
		t.Error("should not have ManagedHostedCluster entity for project-level request")
	}
}

func TestBuildEntityMap_ClusterLevel(t *testing.T) {
	em, err := BuildEntityMap("bob", "prod", "bob@example.com", "ManagedHostedCluster", "cluster-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	clusterUID := cedarlib.NewEntityUID("HCP::ManagedHostedCluster", "cluster-1")
	cluster, ok := em.Get(clusterUID)
	if !ok {
		t.Fatal("expected ManagedHostedCluster entity")
	}

	projectUID := cedarlib.NewEntityUID("HCP::Project", "prod")
	hasParent := false
	for parent := range cluster.Parents.All() {
		if parent == projectUID {
			hasParent = true
			break
		}
	}
	if !hasParent {
		t.Error("ManagedHostedCluster should have Project as parent")
	}
}

func TestBuildEntityMap_WithLabels(t *testing.T) {
	attrs := &ResourceAttributes{
		Labels: map[string]string{"env": "staging", "region": "us-east1"},
	}
	em, err := BuildEntityMap("alice", "my-project", "alice@example.com", "ManagedHostedCluster", "cluster-1", attrs)
	if err != nil {
		t.Fatal(err)
	}

	clusterUID := cedarlib.NewEntityUID("HCP::ManagedHostedCluster", "cluster-1")
	cluster, ok := em.Get(clusterUID)
	if !ok {
		t.Fatal("expected ManagedHostedCluster entity")
	}

	labelsVal, ok := cluster.Attributes.Get("labels")
	if !ok {
		t.Fatal("expected labels attribute")
	}
	labelsRecord, ok := labelsVal.(cedarlib.Record)
	if !ok {
		t.Fatalf("expected labels to be Record, got %T", labelsVal)
	}
	envVal, ok := labelsRecord.Get("env")
	if !ok {
		t.Fatal("expected env label")
	}
	if envVal != cedarlib.String("staging") {
		t.Errorf("env label: got %v, want staging", envVal)
	}
}

func TestBuildEntityMap_NilLabels(t *testing.T) {
	em, err := BuildEntityMap("alice", "my-project", "alice@example.com", "ManagedHostedCluster", "cluster-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	clusterUID := cedarlib.NewEntityUID("HCP::ManagedHostedCluster", "cluster-1")
	cluster, ok := em.Get(clusterUID)
	if !ok {
		t.Fatal("expected ManagedHostedCluster entity")
	}

	labelsVal, ok := cluster.Attributes.Get("labels")
	if !ok {
		t.Fatal("expected labels attribute even with nil attrs")
	}
	_, ok = labelsVal.(cedarlib.Record)
	if !ok {
		t.Fatalf("expected labels to be Record, got %T", labelsVal)
	}
}

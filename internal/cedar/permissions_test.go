package cedar

import "testing"

func TestAllPermissions(t *testing.T) {
	perms := AllPermissions()
	if len(perms) != 7 {
		t.Errorf("expected 7 permissions, got %d", len(perms))
	}
}

func TestValidatePermissions_Valid(t *testing.T) {
	if err := ValidatePermissions([]string{"cluster.get", "cluster.list"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePermissions_Invalid(t *testing.T) {
	err := ValidatePermissions([]string{"cluster.get", "invalid.perm"})
	if err == nil {
		t.Fatal("expected error for invalid permission")
	}
}

func TestMapPermissionsToActions(t *testing.T) {
	actions := MapPermissionsToActions([]string{"cluster.create", "cluster.get"})
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if actions[0] != "CreateManagedHostedCluster" {
		t.Errorf("expected CreateManagedHostedCluster, got %q", actions[0])
	}
	if actions[1] != "GetManagedHostedCluster" {
		t.Errorf("expected GetManagedHostedCluster, got %q", actions[1])
	}
}

func TestIsCollectionPermission(t *testing.T) {
	if !IsCollectionPermission(PermClusterList) {
		t.Error("cluster.list should be a collection permission")
	}
	if !IsCollectionPermission(PermClusterCreate) {
		t.Error("cluster.create should be a collection permission")
	}
	if IsCollectionPermission(PermClusterGet) {
		t.Error("cluster.get should not be a collection permission")
	}
	if IsCollectionPermission(PermClusterDelete) {
		t.Error("cluster.delete should not be a collection permission")
	}
	if IsCollectionPermission(PermPolicyManage) {
		t.Error("policy.manage should not be a collection permission")
	}
}

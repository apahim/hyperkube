package cedar

import "fmt"

const (
	PermClusterCreate     = "cluster.create"
	PermClusterList       = "cluster.list"
	PermClusterGet        = "cluster.get"
	PermClusterUpdate     = "cluster.update"
	PermClusterDelete     = "cluster.delete"
	PermClusterKubeconfig = "cluster.kubeconfig"
	PermPolicyManage      = "policy.manage"
)

var PermissionToAction = map[string]string{
	PermClusterCreate:     "CreateManagedHostedCluster",
	PermClusterList:       "ListManagedHostedClusters",
	PermClusterGet:        "GetManagedHostedCluster",
	PermClusterUpdate:     "UpdateManagedHostedCluster",
	PermClusterDelete:     "DeleteManagedHostedCluster",
	PermClusterKubeconfig: "GetKubeConfig",
	PermPolicyManage:      "ManagePolicies",
}

func AllPermissions() []string {
	return []string{
		PermClusterCreate,
		PermClusterList,
		PermClusterGet,
		PermClusterUpdate,
		PermClusterDelete,
		PermClusterKubeconfig,
		PermPolicyManage,
	}
}

func ValidatePermissions(perms []string) error {
	for _, p := range perms {
		if _, ok := PermissionToAction[p]; !ok {
			return fmt.Errorf("unknown permission: %q", p)
		}
	}
	return nil
}

func MapPermissionsToActions(perms []string) []string {
	actions := make([]string, 0, len(perms))
	for _, p := range perms {
		if a, ok := PermissionToAction[p]; ok {
			actions = append(actions, a)
		}
	}
	return actions
}

var collectionPermissions = map[string]bool{
	PermClusterList:   true,
	PermClusterCreate: true,
}

func IsCollectionPermission(perm string) bool {
	return collectionPermissions[perm]
}

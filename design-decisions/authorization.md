# Authorization Architecture

## Decision

Cedar (via [cedar-go](https://github.com/cedar-policy/cedar-go) v1.8.0) is the authorization engine for the GCP HCP managed service API.

### Why Cedar

- **Policy-as-data**: Cedar policies are declarative, auditable, and can be stored alongside the resources they protect (Kubernetes ConfigMaps / CRDs).
- **Fine-grained conditions**: Native `when`/`unless` clause support enables attribute-based access control (ABAC) on resource labels without custom code.
- **Entity hierarchy**: The `in` operator models project-scoped resources naturally (User in Project, Cluster in Project).
- **Go SDK**: `cedar-go` provides a pure-Go implementation with no external dependencies, suitable for embedding in a controller-runtime binary.

## Permission Model

### Granular Permissions

| Permission | Cedar Action | Scope |
|---|---|---|
| `cluster.create` | `CreateManagedHostedCluster` | Project (collection) |
| `cluster.list` | `ListManagedHostedClusters` | Project (collection) |
| `cluster.get` | `GetManagedHostedCluster` | Cluster or Project |
| `cluster.update` | `UpdateManagedHostedCluster` | Cluster or Project |
| `cluster.delete` | `DeleteManagedHostedCluster` | Cluster or Project |
| `cluster.kubeconfig` | `GetKubeConfig` | Cluster or Project |
| `policy.manage` | `ManagePolicies` | Project |

Permission names follow `<resource>.<verb>` convention. These will be aligned with ROSA Regionality permission semantics when that spec is finalized.

### Entity Model

```
namespace HCP {
    entity Project = {};
    entity User in [Project] = { email: String };
    entity ManagedHostedCluster in [Project] = { labels: Record };
}
```

- **Project**: Scope container, identified by the URL namespace path parameter.
- **User**: Principal, identified by Google JWT `sub` claim. Member of a Project.
- **ManagedHostedCluster**: Resource, child of a Project. Carries `labels` for condition evaluation.

## Roles

### Predefined Roles

| Role | Permissions |
|---|---|
| **Service Admin** | All actions (wildcard) |
| **Cluster Admin** | create, list, get, update, delete, kubeconfig |
| **Cluster Viewer** | list, get |
| **Developer** | list, get, kubeconfig |

Predefined roles are embedded in the binary as Cedar policy templates and cannot be modified at runtime.

### Custom Roles

Customers can define custom roles via the `CustomRole` CRD:

```yaml
apiVersion: hcp.gcp.hypershift.openshift.com/v1alpha1
kind: CustomRole
metadata:
  name: staging-developer
  namespace: my-project
spec:
  permissions:
    - cluster.list
    - cluster.get
    - cluster.kubeconfig
  conditions:
    - 'resource.labels.env == "staging"'
```

Custom roles are namespaced to a project. The `conditions` field accepts Cedar `when` clause bodies that scope permissions to resources matching specific label criteria.

## Attachment Model

Roles (predefined or custom) are bound to users per-project via **attachments**, stored in Kubernetes ConfigMaps (`cedar-attachments-{projectID}` in the `cedar-policies` namespace).

```
POST /authz/namespaces/{namespace}/attachments
{
  "template_name": "cluster-viewer",
  "user_id": "<google-sub-claim>"
}
```

At authorization time, all attachments for the project are resolved into concrete Cedar policies by substituting `?principal` and `?resource` placeholders with the actual user and project entity UIDs.

## Authorization Flow

1. **Request mapping**: HTTP method + URL path maps to a Cedar action and resource via compiled regexes.
2. **Authentication**: Google JWT extracted from `Authorization: Bearer` header, validated against Google JWKS.
3. **Policy resolution**: All attachments for the project are loaded from the ConfigMap, each resolved to concrete Cedar policy text.
4. **Entity construction**: User, Project, and (optionally) ManagedHostedCluster entities built with actual K8s resource labels.
5. **Authorization**: Cedar engine evaluates policies against the request. Default deny if no policies match.

## Conditions

Conditions use Cedar's `when` clause to scope permissions by resource attributes. The `ManagedHostedCluster` entity carries a `labels` record populated from the Kubernetes resource's labels at authorization time.

Examples:
- Region restriction: `resource.labels.region == "us-east1"`
- Environment scoping: `resource.labels.env == "staging"`
- Team restriction: `resource.labels.team == "platform"`

## Identity

Google (GCP) ID tokens are the sole identity mechanism. The JWT `sub` claim identifies the user, and `email` is stored as a Cedar entity attribute. JWKS is cached with a 1-hour TTL.

## Future Work

- **ROSA Regionality alignment**: Permission names and semantics will be aligned with the ROSA Regionality project when that spec is available.
- **Kubeconfig retrieval**: The `GET .../kubeconfig` endpoint currently returns a placeholder. Full implementation will read the HostedCluster kubeconfig Secret.
- **Additional resources**: As new API resources are added (NodePools, etc.), new permissions and entity types will be added to the Cedar schema.

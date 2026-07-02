# End-to-End Test Plan: Placement and Resource Propagation

This document covers manual verification of the Firestore-based desire pipeline:
API server → Firestore → kube-applier → Kubernetes, using two Kind management
clusters and a Firestore emulator.

**Scope:** Placement (least-loaded allocation across two MCs) and resource
propagation (desires written by the API, applied to K8s by kube-applier, status
synced back). The API server writes both Namespace and MHC ApplyDesires so the
kube-applier creates namespaces on the target MC before applying MHC resources.
No HyperShift controllers -- we test the desire pipeline, not the hosted control
plane lifecycle.

## Prerequisites

- `kind` installed
- `kubectl` installed
- `gcloud` CLI installed (for the Firestore emulator and identity tokens)
- `jq` installed
- Go toolchain (to build the API server and kube-applier)
- The kube-applier repo checked out at `../gcp-hcp/experiments/kube-applier-gcp`
  (relative to the hyperkube repo root)

---

## 1. Environment Setup

### 1.1 Create Two Kind Clusters

```bash
kind create cluster --name mc-01 --wait 60s
kind create cluster --name mc-02 --wait 60s
```

- [ ] Both clusters created successfully
- [ ] `kind get clusters` shows `mc-01` and `mc-02`

### 1.2 Install the MHC CRD on Both Clusters

The kube-applier applies ManagedHostedCluster resources to the management clusters,
so the CRD must exist there.

```bash
kubectl --context kind-mc-01 apply -f config/crd/bases/hcp.gcp.hypershift.openshift.com_managedhostedclusters.yaml
kubectl --context kind-mc-02 apply -f config/crd/bases/hcp.gcp.hypershift.openshift.com_managedhostedclusters.yaml
```

- [ ] CRD installed on mc-01
- [ ] CRD installed on mc-02

### 1.3 Create kube-applier Namespace and RBAC on Both Clusters

```bash
KUBE_APPLIER_DIR="../gcp-hcp/experiments/kube-applier-gcp"

for MC in mc-01 mc-02; do
  kubectl --context kind-${MC} create namespace kube-applier-system
  kubectl --context kind-${MC} apply -f ${KUBE_APPLIER_DIR}/hack/manifests/rbac.yaml
done
```

- [ ] Namespace and RBAC created on mc-01
- [ ] Namespace and RBAC created on mc-02

### 1.3.1 Export Per-Cluster Kubeconfig Files

Each kube-applier instance uses the kubeconfig's current-context at startup and
has no `--context` flag. To avoid both instances pointing at the same cluster
after a context switch, export dedicated kubeconfig files:

```bash
kind get kubeconfig --name mc-01 > /tmp/mc-01.kubeconfig
kind get kubeconfig --name mc-02 > /tmp/mc-02.kubeconfig
```

- [ ] `/tmp/mc-01.kubeconfig` created
- [ ] `/tmp/mc-02.kubeconfig` created

### 1.4 Start the Firestore Emulator

In a dedicated terminal:

```bash
export FIRESTORE_EMULATOR_HOST=localhost:8219
gcloud emulators firestore start --host-port="${FIRESTORE_EMULATOR_HOST}"
```

- [ ] Emulator running on `localhost:8219`

### 1.5 Seed Placement and Cedar Data

The placement database needs management cluster documents with capacity, and the
Cedar database needs a global policy granting your Google identity full access.
The seed script handles both in a single invocation.

```bash
export FIRESTORE_EMULATOR_HOST=localhost:8219
export TOKEN=$(gcloud auth print-identity-token)
export USER_ID=$(echo $TOKEN | cut -d. -f2 | base64 -d 2>/dev/null | jq -r .sub)
echo "USER_ID: $USER_ID"

go run ./hack/seed-e2e.go --user-id "$USER_ID"
```

This writes:

**Placement database** (`placement`), collection `managementclusters`:

| Document ID | `capacity` | `allocated` |
|-------------|-----------|------------|
| `mc-01`     | 10        | 0          |
| `mc-02`     | 10        | 0          |

**Cedar database** (`cedar`), collection `global-policies`, document `default`,
field `policies.cedar`:

```cedar
permit (
    principal == HCP::User::"<USER_ID>",
    action,
    resource
);
```

- [ ] `placement/managementclusters/mc-01` exists with capacity=10, allocated=0
- [ ] `placement/managementclusters/mc-02` exists with capacity=10, allocated=0
- [ ] Global policy seeded in `cedar/global-policies/default`

### 1.6 Build the API Server

```bash
make build-apiserver
```

- [ ] `bin/apiserver` built successfully

### 1.7 Build kube-applier

```bash
cd ../gcp-hcp/experiments/kube-applier-gcp
make build
cd -
```

- [ ] `kube-applier-gcp` binary built

---

## 2. Start Services

### 2.1 Start the API Server

In a dedicated terminal:

```bash
export FIRESTORE_EMULATOR_HOST=localhost:8219

./bin/apiserver \
  --addr :8080 \
  --gcp-project test-project \
  --placement-database placement \
  --cedar-database cedar
```

- [ ] API server starts without errors
- [ ] Health check passes:

```bash
curl -s http://localhost:8080/healthz | jq .
```

Expected: `{"status":"ok"}`

### 2.2 Start kube-applier for mc-01

In a dedicated terminal. The API server names databases `mc-{name}-specs` where
the name is the MC document ID (`mc-01`), so the database IDs are `mc-mc-01-specs`
and `mc-mc-01-status`.

Each kube-applier uses the kubeconfig's current-context at startup and has no
`--context` flag. Use the per-cluster kubeconfig files exported in step 1.3.1
to avoid both instances pointing at the same cluster.

```bash
export FIRESTORE_EMULATOR_HOST=localhost:8219
KUBE_APPLIER_DIR="../gcp-hcp/experiments/kube-applier-gcp"

${KUBE_APPLIER_DIR}/kube-applier-gcp \
  --kubeconfig /tmp/mc-01.kubeconfig \
  --namespace kube-applier-system \
  --management-cluster mc-01 \
  --firestore-project test-project \
  --firestore-specs-database mc-mc-01-specs \
  --firestore-status-database mc-mc-01-status \
  --leader-election-id kube-applier-mc-01 \
  --healthz-listen-address :8084 \
  --metrics-listen-address :8085
```

- [ ] kube-applier mc-01 starts and acquires leader lock

### 2.3 Start kube-applier for mc-02

In a dedicated terminal:

```bash
export FIRESTORE_EMULATOR_HOST=localhost:8219
KUBE_APPLIER_DIR="../gcp-hcp/experiments/kube-applier-gcp"

${KUBE_APPLIER_DIR}/kube-applier-gcp \
  --kubeconfig /tmp/mc-02.kubeconfig \
  --namespace kube-applier-system \
  --management-cluster mc-02 \
  --firestore-project test-project \
  --firestore-specs-database mc-mc-02-specs \
  --firestore-status-database mc-mc-02-status \
  --leader-election-id kube-applier-mc-02 \
  --healthz-listen-address :8086 \
  --metrics-listen-address :8087
```

- [ ] kube-applier mc-02 starts and acquires leader lock

---

## 3. Test: Placement and Create

### 3.1 Create First Cluster

```bash
export TOKEN=$(gcloud auth print-identity-token)

curl -s -X POST http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "metadata": {"name": "cluster-a"},
    "spec": {
      "clusterID": "cluster-a-id",
      "versionStreamRef": {"name": "stable"},
      "hostedCluster": {
        "release": {"image": "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"},
        "infraID": "cluster-a-infra"
      }
    }
  }' | jq .
```

- [ ] Returns HTTP 200
- [ ] Response contains `metadata.name: "cluster-a"` and `metadata.namespace: "team-alpha"`

### 3.2 Create Second Cluster

```bash
curl -s -X POST http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "metadata": {"name": "cluster-b"},
    "spec": {
      "clusterID": "cluster-b-id",
      "versionStreamRef": {"name": "stable"},
      "hostedCluster": {
        "release": {"image": "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"},
        "infraID": "cluster-b-infra"
      }
    }
  }' | jq .
```

- [ ] Returns HTTP 200

### 3.3 Create Third Cluster (different namespace)

```bash
curl -s -X POST http://localhost:8080/v1alpha1/namespaces/team-beta/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "metadata": {"name": "cluster-c"},
    "spec": {
      "clusterID": "cluster-c-id",
      "versionStreamRef": {"name": "stable"},
      "hostedCluster": {
        "release": {"image": "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"},
        "infraID": "cluster-c-infra"
      }
    }
  }' | jq .
```

- [ ] Returns HTTP 200

### 3.4 Verify Placement Distribution

Both MCs started with `allocated=0, capacity=10`. The least-loaded algorithm
should spread clusters across MCs. After 3 creates, one MC should have 2 and
the other should have 1.

```bash
# Check which MC each cluster landed on via API server logs
# Look for placement log lines showing the chosen MC for each cluster
```

- [ ] Clusters are distributed across both mc-01 and mc-02
- [ ] Not all 3 clusters landed on the same MC

### 3.5 Verify Duplicate Create is Rejected

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X POST http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "metadata": {"name": "cluster-a"},
    "spec": {
      "clusterID": "cluster-a-id",
      "versionStreamRef": {"name": "stable"},
      "hostedCluster": {
        "release": {"image": "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"},
        "infraID": "cluster-a-infra"
      }
    }
  }'
```

- [ ] Returns HTTP 409 (AlreadyExists)

---

## 4. Test: Resource Propagation (kube-applier -> K8s)

After the API server writes desires to Firestore, the kube-applier agents should
pick them up and create namespaces and ManagedHostedCluster CRs in their
respective Kind clusters.

### 4.1 Verify Namespaces and MHC Resources Exist in Kind

Wait a few seconds for kube-applier to reconcile, then check:

```bash
echo "=== Namespaces ==="
kubectl --context kind-mc-01 get ns team-alpha team-beta 2>&1 || true
kubectl --context kind-mc-02 get ns team-alpha team-beta 2>&1 || true

echo "=== MHC Resources ==="
kubectl --context kind-mc-01 get managedhostedclusters -A -o json | \
  jq '.items[] | {name: .metadata.name, namespace: .metadata.namespace}'
kubectl --context kind-mc-02 get managedhostedclusters -A -o json | \
  jq '.items[] | {name: .metadata.name, namespace: .metadata.namespace}'
```

- [ ] Namespaces created on the MCs that host clusters in those namespaces
- [ ] cluster-a exists as a MHC CR in one of the Kind clusters
- [ ] cluster-b exists as a MHC CR in one of the Kind clusters
- [ ] cluster-c exists in one of the Kind clusters (namespace `team-beta`)
- [ ] Total MHC count across both clusters equals 3

### 4.2 Verify CR Spec Matches the Submitted Body

```bash
# On whichever MC hosts cluster-a (e.g., mc-01):
kubectl --context kind-mc-01 get managedhostedcluster cluster-a -n team-alpha -o json | \
  jq '{clusterID: .spec.clusterID, infraID: .spec.hostedCluster.infraID, image: .spec.hostedCluster.release.image}'
```

- [ ] `clusterID` is `"cluster-a-id"`
- [ ] `infraID` is `"cluster-a-infra"`
- [ ] `image` is `"quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"`

---

## 5. Test: Status Sync (K8s -> Firestore -> API)

The kube-applier reads live MHC resources and writes their state back to the
status database as ReadDesire `status.kubeContent`. The API server's Get handler
reads from the status DB. It also surfaces a top-level `desireStatus` field
containing the kube-applier's reconciliation status for both the ApplyDesire
(spec application) and the ReadDesire (live object watch).

### 5.1 Get Cluster via API (after kube-applier sync)

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/cluster-a \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns HTTP 200
- [ ] Response contains `apiVersion`, `kind`, `metadata`, `spec`
- [ ] If kube-applier has synced status, the response reflects the live K8s
      object (may include server-set fields like `metadata.creationTimestamp`)

### 5.2 Verify desireStatus on Get

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/cluster-a \
  -H "Authorization: Bearer $TOKEN" | jq '.desireStatus'
```

Expected structure:

```json
{
  "apply": {
    "conditions": [
      { "type": "Successful", "status": "True", "reason": "NoErrors", ... },
      { "type": "Degraded",   "status": "False", "reason": "NoErrors", ... }
    ],
    "observedDesireUpdateTime": "...",
    "appliedResourceGeneration": 1
  },
  "read": {
    "conditions": [
      { "type": "Successful", "status": "True", "reason": "NoErrors", ... }
    ],
    "observedDesireUpdateTime": "..."
  }
}
```

- [ ] `desireStatus` is present in the response
- [ ] `desireStatus.apply` contains `conditions`, `observedDesireUpdateTime`,
      and `appliedResourceGeneration`
- [ ] `desireStatus.apply.conditions` includes `Successful=True` and
      `Degraded=False`
- [ ] `desireStatus.read` contains `conditions` and `observedDesireUpdateTime`
- [ ] `desireStatus.read.conditions` includes `Successful=True`

### 5.3 List Clusters in a Namespace

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" | jq '.items | length'
```

- [ ] Returns HTTP 200 with 2 items (cluster-a, cluster-b)

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/team-beta/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" | jq '.items | length'
```

- [ ] Returns HTTP 200 with 1 item (cluster-c)

### 5.4 Verify desireStatus on List

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" | \
  jq '.items[] | {name: .metadata.name, hasDesireStatus: (.desireStatus != null)}'
```

- [ ] Every item in the list includes `desireStatus`
- [ ] Each item's `desireStatus` has both `apply` and `read` sections

---

## 6. Test: Update

### 6.1 Update a Cluster's Spec

```bash
curl -s -X PUT http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/cluster-a \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "metadata": {"name": "cluster-a"},
    "spec": {
      "clusterID": "cluster-a-id",
      "versionStreamRef": {"name": "fast"},
      "hostedCluster": {
        "release": {"image": "quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64"},
        "infraID": "cluster-a-infra"
      }
    }
  }' | jq .
```

- [ ] Returns HTTP 200

### 6.2 Verify Update Propagated to K8s

Wait a few seconds for kube-applier to reconcile:

```bash
# On the MC hosting cluster-a:
kubectl --context kind-mc-01 get managedhostedcluster cluster-a -n team-alpha -o json | \
  jq '{image: .spec.hostedCluster.release.image, versionStream: .spec.versionStreamRef.name}'
```

- [ ] `image` is `"quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64"`
- [ ] `versionStreamRef.name` is `"fast"`

### 6.3 Verify Update via API Get

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/cluster-a \
  -H "Authorization: Bearer $TOKEN" | jq '.spec.hostedCluster.release.image'
```

- [ ] Returns the updated image

---

## 7. Test: Delete

### 7.1 Delete a Cluster

```bash
curl -s -X DELETE http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/cluster-b \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns HTTP 200

### 7.2 Verify Cluster Removed from K8s

Wait for kube-applier to reconcile the DeleteDesire:

```bash
kubectl --context kind-mc-01 get managedhostedcluster cluster-b -n team-alpha 2>&1 || true
kubectl --context kind-mc-02 get managedhostedcluster cluster-b -n team-alpha 2>&1 || true
```

- [ ] cluster-b no longer exists in either Kind cluster (NotFound)

### 7.3 Verify Cluster Removed from API

```bash
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/cluster-b \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns HTTP 404

### 7.4 Verify List Reflects Deletion

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" | jq '.items | length'
```

- [ ] Returns 1 (only cluster-a)

### 7.5 Verify Placement Released Capacity

After delete, the MC's `allocated` count should decrement. Create a new cluster
and verify placement still works:

```bash
curl -s -X POST http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "metadata": {"name": "cluster-d"},
    "spec": {
      "clusterID": "cluster-d-id",
      "versionStreamRef": {"name": "stable"},
      "hostedCluster": {
        "release": {"image": "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"},
        "infraID": "cluster-d-infra"
      }
    }
  }' | jq .
```

- [ ] Returns HTTP 200
- [ ] cluster-d is placed on the MC with fewer allocations (the one that released
      cluster-b's capacity)

---

## 8. Test: Cross-Namespace Isolation

### 8.1 List Returns Only the Requested Namespace

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" | jq '[.items[].metadata.name]'
```

- [ ] Returns only `team-alpha` clusters (cluster-a, cluster-d)
- [ ] Does NOT include cluster-c (which is in `team-beta`)

### 8.2 Get from Wrong Namespace Returns 404

```bash
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/cluster-c \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns HTTP 404 (cluster-c is in team-beta, not team-alpha)

---

## 9. Test: Error Cases

### 9.1 Get Non-Existent Cluster

```bash
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/nonexistent \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns HTTP 404

### 9.2 Update Non-Existent Cluster

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X PUT http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/nonexistent \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "metadata": {"name": "nonexistent"},
    "spec": {
      "clusterID": "x",
      "versionStreamRef": {"name": "stable"},
      "hostedCluster": {"release": {"image": "img:v1"}, "infraID": "x"}
    }
  }'
```

- [ ] Returns HTTP 404

### 9.3 Delete Non-Existent Cluster

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X DELETE http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters/nonexistent \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns HTTP 404

### 9.4 Create with Invalid Body

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X POST http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata": {"name": "bad"}, "spec": {}}'
```

- [ ] Returns HTTP 400 (OpenAPI validation rejects missing required fields)

### 9.5 Unauthenticated Request

```bash
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/team-alpha/managedhostedclusters
```

- [ ] Returns HTTP 401

---

## 10. Cleanup

```bash
# Stop API server and kube-applier processes (Ctrl+C in their terminals)
# Stop Firestore emulator (Ctrl+C)

kind delete cluster --name mc-01
kind delete cluster --name mc-02
```

- [ ] All processes stopped
- [ ] Kind clusters deleted

---

## Implementation Notes

### Seed Script

The seed script (`hack/seed-e2e.go`) populates the Firestore emulator:

1. Writes two `managementclusters` documents (mc-01, mc-02 with capacity=10) to
   the `placement` Firestore database
2. When `--user-id` is provided, writes a `global-policies/default` document to
   the `cedar` Firestore database with a permit-all policy for that user

The script uses `option.WithoutAuthentication()` + insecure gRPC credentials +
`FIRESTORE_EMULATOR_HOST` (same pattern as `internal/cedar/testutil_test.go`).
Project ID: `test-project`.

### Per-Cluster Kubeconfigs

The kube-applier binary uses the kubeconfig's `current-context` at startup and
does not accept a `--context` flag. When running two instances on the same host,
use `kind get kubeconfig --name <cluster>` to produce isolated kubeconfig files.
Without this, switching context between starts causes both instances to connect
to the same cluster.

### Namespace Desires

The API server's Create handler writes a Namespace ApplyDesire before the MHC
ApplyDesire. The namespace desire uses an empty `ClusterID` (namespace is a
shared resource) and swallows `AlreadyExists` errors (idempotent across multiple
clusters in the same namespace). The kube-applier SSA-applies the Namespace,
then the MHC. If the MHC desire is reconciled before the namespace exists, it
retries with backoff until the namespace appears.

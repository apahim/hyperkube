# Authorization Test Plan

This document covers manual and automated verification of the Cedar authorization system for GCP-783.

## Prerequisites

- A local Kubernetes cluster (kind or minikube)
- `kubectl` configured against the cluster
- `gcloud` CLI authenticated with a Google identity
- CRDs installed (`make install` + CustomRole CRD)
- Cedar policy namespace created (`kubectl create namespace cedar-policies`)
- API server built and running

### Environment Setup

```bash
# 1. Create cluster and install CRDs
kind create cluster --name hyperkube
make install
kubectl apply -f config/crd/bases/hcp.gcp.hypershift.openshift.com_customroles.yaml

# 2. Create namespaces
kubectl create namespace cedar-policies
kubectl create namespace test-project

# 3. Build and run the API server
go build -o apiserver ./cmd/apiserver/
./apiserver --addr :8080 --cedar-policy-namespace cedar-policies \
  > /tmp/apiserver.stdout 2>/tmp/apiserver.stderr &

# 4. Get a Google identity token and user ID
export TOKEN=$(gcloud auth print-identity-token)
export USER_ID=$(echo $TOKEN | cut -d. -f2 | base64 -d 2>/dev/null | jq -r .sub)
echo "USER_ID: $USER_ID"
```

> **Important:** All `/authz/namespaces/` endpoints require `ManagePolicies` authorization.
> The test plan bootstraps access via a global policy ConfigMap before testing
> attachment management. Sections 1-4 run before bootstrap; sections 5+ require it.

---

## 1. Infrastructure Endpoints (No Auth Required)

These endpoints bypass authorization entirely.

### 1.1 Health Check

```bash
curl -s http://localhost:8080/healthz | jq .
```

- [ ] Returns `200` with `{"status":"ok"}`

### 1.2 OpenAPI Spec

```bash
curl -s http://localhost:8080/openapi.yaml | head -5
```

- [ ] Returns `200` with YAML content starting with `components:`

### 1.3 Template Listing (Read-Only, No Auth)

```bash
curl -s http://localhost:8080/authz/templates | jq .
```

- [ ] Returns `200` with 4 templates: `cluster-admin`, `cluster-viewer`, `developer`, `service-admin`

### 1.4 Template Detail

```bash
curl -s http://localhost:8080/authz/templates/cluster-viewer | jq .
```

- [ ] Returns `200` with `name: "cluster-viewer"` and non-empty `policy_text`

```bash
curl -s http://localhost:8080/authz/templates/nonexistent | jq .
```

- [ ] Returns `404`

---

## 2. OpenAPI Request Validation

### 2.1 Missing Required Field

```bash
curl -s -X POST http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"test"}}' | jq .
```

- [ ] Returns `400` (validation rejects before auth check -- missing `spec`)

### 2.2 Valid Body, No Auth

```bash
curl -s -X POST http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"test"},"spec":{"clusterID":"c1","versionStreamRef":{"name":"vs1"}}}' | jq .
```

- [ ] Returns `401` (valid body passes validation, rejected by auth)

---

## 3. Authentication

### 3.1 No Token

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters | jq .
```

- [ ] Returns `401` with "Authentication failed" message

### 3.2 Invalid Token

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer invalid-token" | jq .
```

- [ ] Returns `401`

### 3.3 Wrong Scheme

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Basic dXNlcjpwYXNz" | jq .
```

- [ ] Returns `401`

---

## 4. Default Deny (No Policies)

> **Note:** Run this section BEFORE deploying the global policy ConfigMap (Section 5).

### 4.1 Authenticated but No Policies

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns `403` with "no policies configured"

### 4.2 ManagePolicies Also Denied Without Policies

```bash
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `403` (cannot manage policies on a project with no policies -- bootstrap problem)

---

## 5. Bootstrap Flow (Global Policies)

The global policy ConfigMap grants a service account (or the tester) `ManagePolicies`
on all projects. This solves the bootstrap problem: the first `service-admin` attachment
for a new project can be created through the API.

### 5.1 Deploy Global Policy ConfigMap

```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: cedar-global-policies
  namespace: cedar-policies
data:
  policies.cedar: |
    permit (
        principal == HCP::User::"$USER_ID",
        action == HCP::Action::"ManagePolicies",
        resource
    );
EOF
```

- [ ] ConfigMap created successfully

### 5.2 ManagePolicies Now Succeeds on Fresh Project

```bash
curl -s http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns `200` with empty array `[]`

### 5.3 Bootstrap First Service Admin

```bash
curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"service-admin\",\"user_id\":\"$USER_ID\"}" | jq .
```

- [ ] Returns `201` with attachment details

### 5.4 Global Policy Does Not Grant Cluster Operations

```bash
# Remove service-admin to isolate global policy behavior
curl -s http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[].id' | while read id; do
  curl -s -X DELETE "http://localhost:8080/authz/namespaces/test-project/attachments/$id" \
    -H "Authorization: Bearer $TOKEN"
done

curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `403` (global policy only grants ManagePolicies, not cluster operations)

### 5.5 Without Global Policy ConfigMap

```bash
kubectl delete configmap cedar-global-policies -n cedar-policies

curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"template_name":"service-admin","user_id":"someone"}'
```

- [ ] Returns `403` (no global policy, no project policies)

```bash
# Restore global policy for remaining tests
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: cedar-global-policies
  namespace: cedar-policies
data:
  policies.cedar: |
    permit (
        principal == HCP::User::"$USER_ID",
        action == HCP::Action::"ManagePolicies",
        resource
    );
EOF
```

---

## 6. Predefined Roles

> **Note:** All attachment management commands below use `Authorization: Bearer $TOKEN`
> because `/authz/namespaces/` endpoints require `ManagePolicies` (granted by the
> global policy ConfigMap deployed in Section 5).

### 6.1 Cluster Viewer Role

```bash
# Clean up and attach cluster-viewer
curl -s http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[].id' | while read id; do
  curl -s -X DELETE "http://localhost:8080/authz/namespaces/test-project/attachments/$id" \
    -H "Authorization: Bearer $TOKEN"
done

curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"cluster-viewer\",\"user_id\":\"$USER_ID\"}" | jq .
```

- [ ] Returns `201` with attachment details

```bash
# LIST should succeed
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# GET should succeed (need a cluster — create one via kubectl or use service-admin first)
# If no cluster exists, expect 404 (authz passed, resource not found)
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/viewer-test \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200` if cluster exists, `404` if not (not `403`)

```bash
# CREATE should be denied
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"test"},"spec":{"clusterID":"c1","versionStreamRef":{"name":"vs1"}}}'
```

- [ ] Returns `403`

```bash
# UPDATE should be denied
curl -s -o /dev/null -w "%{http_code}" -X PUT \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/test \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"test"},"spec":{"clusterID":"c1","versionStreamRef":{"name":"vs1"}}}'
```

- [ ] Returns `403`

```bash
# DELETE should be denied
curl -s -o /dev/null -w "%{http_code}" -X DELETE \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/test \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `403`

```bash
# Kubeconfig should be denied
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/test/kubeconfig \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `403`

### 6.2 Developer Role

```bash
# First, create a cluster using service-admin so we can test GET/kubeconfig later
curl -s http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[].id' | while read id; do
  curl -s -X DELETE "http://localhost:8080/authz/namespaces/test-project/attachments/$id" \
    -H "Authorization: Bearer $TOKEN"
done

curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"service-admin\",\"user_id\":\"$USER_ID\"}" > /dev/null

curl -s -X POST http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"dev-test"},"spec":{"clusterID":"d1","versionStreamRef":{"name":"vs1"}}}' > /dev/null

# Now switch to developer role
curl -s http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[].id' | while read id; do
  curl -s -X DELETE "http://localhost:8080/authz/namespaces/test-project/attachments/$id" \
    -H "Authorization: Bearer $TOKEN"
done

curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"developer\",\"user_id\":\"$USER_ID\"}" | jq .
```

- [ ] Returns `201`

```bash
# LIST should succeed
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# GET should succeed
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/dev-test \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# CREATE should be denied
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"test"},"spec":{"clusterID":"c1","versionStreamRef":{"name":"vs1"}}}'
```

- [ ] Returns `403`

```bash
# UPDATE should be denied
curl -s -o /dev/null -w "%{http_code}" -X PUT \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/dev-test \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"dev-test"},"spec":{"clusterID":"d1","versionStreamRef":{"name":"vs1"}}}'
```

- [ ] Returns `403`

```bash
# DELETE should be denied
curl -s -o /dev/null -w "%{http_code}" -X DELETE \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/dev-test \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `403`

```bash
# Kubeconfig should succeed (cluster was created above)
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/dev-test/kubeconfig \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

### 6.3 Cluster Admin Role

```bash
# Clean up and attach cluster-admin
curl -s http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[].id' | while read id; do
  curl -s -X DELETE "http://localhost:8080/authz/namespaces/test-project/attachments/$id" \
    -H "Authorization: Bearer $TOKEN"
done

curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"cluster-admin\",\"user_id\":\"$USER_ID\"}" | jq .
```

- [ ] Returns `201`

```bash
# CREATE should succeed
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"admin-test"},"spec":{"clusterID":"c1","versionStreamRef":{"name":"vs1"}}}'
```

- [ ] Returns `201`

```bash
# GET should succeed
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/admin-test \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# UPDATE should succeed
curl -s -o /dev/null -w "%{http_code}" -X PUT \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/admin-test \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"admin-test"},"spec":{"clusterID":"c1-updated","versionStreamRef":{"name":"vs1"}}}'
```

- [ ] Returns `200`

```bash
# Kubeconfig should succeed
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/admin-test/kubeconfig \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# DELETE should succeed
curl -s -o /dev/null -w "%{http_code}" -X DELETE \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/admin-test \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# Policy management should be denied (cluster-admin != service-admin)
# Note: this tests the project-level attachment Cedar policy, NOT the global policy.
# The global policy grants ManagePolicies to the tester, so to properly test this,
# temporarily remove the global ConfigMap or use a second user without global access.
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"template_name":"cluster-viewer","user_id":"someone-else"}'
```

- [ ] Returns `403` (when tested without global policy) or `201` (if global policy is active for tester)

### 6.4 Service Admin Role

```bash
# Clean up and attach service-admin
curl -s http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[].id' | while read id; do
  curl -s -X DELETE "http://localhost:8080/authz/namespaces/test-project/attachments/$id" \
    -H "Authorization: Bearer $TOKEN"
done

curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"service-admin\",\"user_id\":\"$USER_ID\"}" | jq .
```

- [ ] Returns `201`

```bash
# LIST should succeed
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# CREATE should succeed
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"sa-test"},"spec":{"clusterID":"sa1","versionStreamRef":{"name":"vs1"}}}'
```

- [ ] Returns `201`

```bash
# GET should succeed
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/sa-test \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# UPDATE should succeed
curl -s -o /dev/null -w "%{http_code}" -X PUT \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/sa-test \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"sa-test"},"spec":{"clusterID":"sa1-updated","versionStreamRef":{"name":"vs1"}}}'
```

- [ ] Returns `200`

```bash
# Kubeconfig should succeed
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/sa-test/kubeconfig \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# DELETE should succeed
curl -s -o /dev/null -w "%{http_code}" -X DELETE \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/sa-test \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# Policy management should succeed
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"template_name":"cluster-viewer","user_id":"someone-else"}'
```

- [ ] Returns `201`

---

## 7. Predefined Role Permission Matrix

| Operation | Service Admin | Cluster Admin | Cluster Viewer | Developer |
|---|:---:|:---:|:---:|:---:|
| List clusters | [ ] Allow | [ ] Allow | [ ] Allow | [ ] Allow |
| Get cluster | [ ] Allow | [ ] Allow | [ ] Allow | [ ] Allow |
| Create cluster | [ ] Allow | [ ] Allow | [ ] Deny | [ ] Deny |
| Update cluster | [ ] Allow | [ ] Allow | [ ] Deny | [ ] Deny |
| Delete cluster | [ ] Allow | [ ] Allow | [ ] Deny | [ ] Deny |
| Get kubeconfig | [ ] Allow | [ ] Allow | [ ] Deny | [ ] Allow |
| Manage policies | [ ] Allow | [ ] Deny | [ ] Deny | [ ] Deny |

> **Note:** Every operation in this matrix is exercised by the curl commands in sections 6.1–6.4.
> To test the "Manage policies" row accurately, the tester must NOT have global
> `ManagePolicies` access. Either use a second user identity or temporarily remove the
> global policy ConfigMap while testing that row.

---

## 8. Custom Roles (via CRD)

### 8.1 Create a Custom Role

```bash
cat <<EOF | kubectl apply -f -
apiVersion: hcp.gcp.hypershift.openshift.com/v1alpha1
kind: CustomRole
metadata:
  name: read-only-us-east
  namespace: test-project
spec:
  description: "Read-only access restricted to us-east1 clusters"
  permissions:
    - cluster.list
    - cluster.get
  conditions:
    - 'resource.labels.region == "us-east1"'
EOF
```

- [ ] CustomRole created successfully

### 8.2 Verify Custom Role Appears in Role Listing

```bash
curl -s http://localhost:8080/authz/namespaces/test-project/roles \
  -H "Authorization: Bearer $TOKEN" | jq '.[].name'
```

- [ ] Lists 5 roles (4 predefined + `read-only-us-east`)

```bash
curl -s http://localhost:8080/authz/namespaces/test-project/roles/read-only-us-east \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns `200` with `predefined: false` and the correct permissions/conditions

### 8.3 Attach Custom Role and Test

```bash
# Clean up and attach custom role
curl -s http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[].id' | while read id; do
  curl -s -X DELETE "http://localhost:8080/authz/namespaces/test-project/attachments/$id" \
    -H "Authorization: Bearer $TOKEN"
done

curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"read-only-us-east\",\"user_id\":\"$USER_ID\"}" | jq .
```

- [ ] Returns `201`

```bash
# LIST should succeed (collection actions are split into a separate condition-free policy)
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# CREATE should be denied (not in custom role permissions)
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"custom-test"},"spec":{"clusterID":"cr1","versionStreamRef":{"name":"vs1"}}}'
```

- [ ] Returns `403`

```bash
# GET a cluster with region=us-east1 label should succeed
# (Requires a ManagedHostedCluster with labels.region=us-east1 to exist)
```

- [ ] Returns `200` for clusters with `region=us-east1` label

```bash
# GET a cluster without the matching label should be denied
```

- [ ] Returns `403` for clusters without `region=us-east1` label

### 8.4 Invalid Custom Role

```bash
cat <<EOF | kubectl apply -f -
apiVersion: hcp.gcp.hypershift.openshift.com/v1alpha1
kind: CustomRole
metadata:
  name: invalid-perms
  namespace: test-project
spec:
  permissions:
    - cluster.get
    - invalid.permission
EOF
```

```bash
# Attempting to attach should fail validation
curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"invalid-perms\",\"user_id\":\"$USER_ID\"}" | jq .
```

- [ ] Returns `400` with error about unknown permission

---

## 9. Kubeconfig Endpoint

### 9.1 Stub Response

Requires a cluster to exist and appropriate permissions.

```bash
# Ensure service-admin for test-project
curl -s http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[].id' | while read id; do
  curl -s -X DELETE "http://localhost:8080/authz/namespaces/test-project/attachments/$id" \
    -H "Authorization: Bearer $TOKEN"
done

curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"service-admin\",\"user_id\":\"$USER_ID\"}" | jq .

# Create a cluster
curl -s -X POST http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"name":"kc-test"},"spec":{"clusterID":"kc1","versionStreamRef":{"name":"vs1"}}}' | jq .

# Get kubeconfig
curl -s http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/kc-test/kubeconfig \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns `200` with placeholder kubeconfig containing `kind: Config` and `status: placeholder`

### 9.2 Nonexistent Cluster

```bash
curl -s http://localhost:8080/v1alpha1/namespaces/test-project/managedhostedclusters/nonexistent/kubeconfig \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns `404`

---

## 10. Cross-Project Isolation

### 10.1 Policies Don't Leak Across Projects

```bash
# Create namespaces
kubectl create namespace project-a 2>/dev/null || true
kubectl create namespace project-b 2>/dev/null || true

# Attach service-admin for project-a
curl -s -X POST http://localhost:8080/authz/namespaces/project-a/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"service-admin\",\"user_id\":\"$USER_ID\"}" | jq .

# Access project-a should succeed
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/project-a/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# Access project-b (no attachment) should be denied
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/project-b/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `403`

---

## 11. Attachment Management

> **Note:** All commands in this section require `ManagePolicies` authorization.
> The tester must have `service-admin` on `test-project` or global `ManagePolicies`
> via the global policy ConfigMap.

### 11.1 List Attachments (Empty)

```bash
# Use a project where caller has ManagePolicies but no attachments
curl -s http://localhost:8080/authz/namespaces/project-b/attachments \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns `200` with empty array `[]`

### 11.2 Create Attachment with Invalid Template

```bash
curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"template_name":"nonexistent","user_id":"alice"}' | jq .
```

- [ ] Returns `400` with "not found" error

### 11.3 Create Attachment with Missing Fields

```bash
curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"template_name":"cluster-viewer"}' | jq .
```

- [ ] Returns `400` with "required" error

### 11.4 Delete Attachment

```bash
# Create and then delete
ATT_ID=$(curl -s -X POST http://localhost:8080/authz/namespaces/test-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"template_name":"cluster-viewer","user_id":"temp-user"}' | jq -r .id)

curl -s -X DELETE "http://localhost:8080/authz/namespaces/test-project/attachments/$ATT_ID" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns `200` with `{"status":"deleted"}`

### 11.5 Delete Nonexistent Attachment

```bash
curl -s -X DELETE http://localhost:8080/authz/namespaces/test-project/attachments/nonexistent \
  -H "Authorization: Bearer $TOKEN" | jq .
```

- [ ] Returns `404`

---

## 12. Request Logging

While running any of the above tests, verify the API server stderr output.

```bash
tail -10 /tmp/apiserver.stderr
```

- [ ] Each request produces a structured log line
- [ ] Log includes: `method`, `path`, `status`, `duration`
- [ ] Duration is a reasonable value (not zero, not negative)

---

## 13. Bootstrap with Marketplace Handler SA

This section tests the production bootstrap flow where a Google Cloud service account
(the "marketplace handler") creates the first `service-admin` attachment for a newly
onboarded project.

### 13.1 Configure the Marketplace Handler SA

```bash
# Get the SA's identity token and subject ID
export BOOTSTRAP_TOKEN=$(gcloud auth print-identity-token \
  --impersonate-service-account=MARKETPLACE_SA@PROJECT.iam.gserviceaccount.com)
export BOOTSTRAP_SA_ID=$(echo $BOOTSTRAP_TOKEN | cut -d. -f2 | base64 -d 2>/dev/null | jq -r .sub)
echo "Bootstrap SA ID: $BOOTSTRAP_SA_ID"
```

### 13.2 Deploy Global Policy for the SA

```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: cedar-global-policies
  namespace: cedar-policies
data:
  policies.cedar: |
    permit (
        principal == HCP::User::"$BOOTSTRAP_SA_ID",
        action == HCP::Action::"ManagePolicies",
        resource
    );
EOF
```

- [ ] ConfigMap created successfully

### 13.3 Bootstrap SA Creates First Service Admin

```bash
kubectl create namespace onboarding-project 2>/dev/null || true

curl -s -X POST http://localhost:8080/authz/namespaces/onboarding-project/attachments \
  -H "Authorization: Bearer $BOOTSTRAP_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"template_name\":\"service-admin\",\"user_id\":\"$USER_ID\"}" | jq .
```

- [ ] Returns `201` with attachment details

### 13.4 Bootstrap SA Cannot Access Cluster Operations

```bash
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/onboarding-project/managedhostedclusters \
  -H "Authorization: Bearer $BOOTSTRAP_TOKEN"
```

- [ ] Returns `403` (bootstrap SA only has ManagePolicies, not cluster operations)

### 13.5 Customer Admin Can Now Operate

```bash
# The customer admin (USER_ID) was granted service-admin by the bootstrap SA
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8080/v1alpha1/namespaces/onboarding-project/managedhostedclusters \
  -H "Authorization: Bearer $TOKEN"
```

- [ ] Returns `200`

```bash
# Customer admin can also manage policies
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/authz/namespaces/onboarding-project/attachments \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"template_name":"cluster-viewer","user_id":"team-member"}'
```

- [ ] Returns `201`

### 13.6 Revoking Global Policy Blocks Bootstrap SA

```bash
kubectl delete configmap cedar-global-policies -n cedar-policies

curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://localhost:8080/authz/namespaces/onboarding-project/attachments \
  -H "Authorization: Bearer $BOOTSTRAP_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"template_name":"service-admin","user_id":"another-admin"}'
```

- [ ] Returns `403`

---

## 14. Automated Test Coverage

Run the automated test suite to verify unit and integration test coverage.

```bash
go test -v -count=1 ./internal/cedar/... ./internal/apiserver/...
```

- [ ] All 58 tests pass
- [ ] No race conditions (`go test -race ./internal/cedar/... ./internal/apiserver/...`)

```bash
make lint
```

- [ ] 0 lint issues

```bash
go build ./cmd/apiserver/ && go build ./cmd/main.go
```

- [ ] Both binaries compile without errors

---

## Cleanup

```bash
kind delete cluster --name hyperkube
```

# gcp-hcp-backend

A Kubernetes operator and REST API server for managing HyperShift hosted clusters on GCP. Built with [Kubebuilder](https://book.kubebuilder.io/) v4 and [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime).

The operator uses a **typed spec** approach: a `ManagedHostedCluster` embeds a full [HyperShift](https://github.com/openshift/hypershift) HostedCluster spec under `.spec.hostedCluster`, a `VersionStream` tracks the target OCP version for groups of clusters and resolves it to a release image via the [Cincinnati update service](https://api.openshift.com/api/upgrades_info/v1/graph). A dedicated controller reads the embedded spec, overrides the release image with the Cincinnati-resolved value, and creates/owns the real HostedCluster CR.

A **REST API server** (`cmd/apiserver/`) exposes `ManagedHostedCluster` resources over HTTP, allowing customers to create and manage clusters without direct Kubernetes API access. Each REST request is proxied to the Kubernetes API as the corresponding CR operation.

Each CRD has its own independent controller with configurable concurrent reconciliation, so adding new CRDs does not create bottlenecks.

---

## Table of Contents

- [Architecture](#architecture)
- [Custom Resource Definitions](#custom-resource-definitions)
  - [ManagedHostedCluster](#managedhostedcluster)
  - [VersionStream](#versionstream)
  - [UpgradeSchedule](#upgradeschedule)
- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
  - [Build](#build)
  - [Run Locally](#run-locally)
  - [Deploy to a Cluster](#deploy-to-a-cluster)
  - [Create a ManagedHostedCluster](#create-a-managedhostedcluster)
- [REST API Server](#rest-api-server)
  - [Running the API Server](#running-the-api-server)
  - [API Endpoints](#api-endpoints)
  - [Examples](#examples)
- [OpenAPI Spec Generation](#openapi-spec-generation)
- [Development](#development)
  - [Project Structure](#project-structure)
  - [Make Targets](#make-targets)
  - [Code Generation](#code-generation)
  - [Adding a New CRD and Controller](#adding-a-new-crd-and-controller)
  - [Upgrading HyperShift Types](#upgrading-hypershift-types)
- [Testing](#testing)
  - [Unit Tests](#unit-tests)
  - [End-to-End Tests](#end-to-end-tests)
- [Configuration](#configuration)
  - [Manager Flags](#manager-flags)
  - [Metrics](#metrics)
  - [Leader Election](#leader-election)
- [Security](#security)
- [License](#license)

---

## Architecture

```
                     Cincinnati API
                     (api.openshift.com)
                            │
                            │ queries graph
                            v
  VersionStream (cluster-scoped)
  ┌──────────────────────────┐
  │ spec:                    │
  │   targetVersion: "4.16"  │
  │   channelGroup: "stable" │
  │ status:                  │
  │   releaseImage: quay/... │
  │   resolvedVersion: 4.16.3│
  └────────────┬─────────────┘
               │
               │ referenced by
               │
  ManagedHostedCluster (namespaced)
  ┌──────────────────────────────┐
  │ clusterID: "my-cluster"      │
  │ versionStreamRef: stable     │
  │ hostedCluster:               │
  │   release:                   │
  │     image: quay/...          │
  │   infraID: my-cluster        │
  │   platform: { type: None }   │
  │   networking: { ... }        │
  │   etcd: { ... }              │
  └────────────┬─────────────────┘
               │
               v
  ┌───────────────────────────────────┐
  │    HostedCluster Controller       │
  │                                   │
  │  1. Fetch ManagedHostedCluster    │
  │  2. Fetch VersionStream           │
  │  3. Wait for resolved releaseImage│
  │  4. Build HostedCluster from spec │
  │  5. Override release image        │
  │  6. Create/update HostedCluster CR│
  └──────────────┬────────────────────┘
                 │ creates & owns
                 v
  ┌───────────────────────────────────┐
  │  HostedCluster CR (HyperShift)   │
  │  hypershift.openshift.io/v1beta1 │
  └───────────────────────────────────┘
```

Each controller runs in its own goroutine on a shared manager. Controllers never share a reconcile loop, so one slow controller cannot block another. `MaxConcurrentReconciles` controls how many instances of the same Kind can reconcile in parallel (the workqueue guarantees a single object is never processed concurrently by two goroutines).

---

## Custom Resource Definitions

All CRDs use the API group `hcp.gcp.hypershift.openshift.com/v1alpha1`.

### ManagedHostedCluster

**Scope:** Namespaced

Represents a managed hosted cluster. Embeds the full HyperShift HostedCluster configuration under `.spec.hostedCluster`. References a VersionStream to determine the target OCP version and optionally an UpgradeSchedule.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `clusterID` | `string` | Yes | Unique identifier for this cluster. |
| `versionStreamRef.name` | `string` | Yes | Name of the cluster-scoped VersionStream resource. |
| `upgradeScheduleRef.name` | `string` | No | Name of an UpgradeSchedule in the same namespace. If unset, upgrades apply immediately. |
| `hostedCluster` | `HostedClusterSpec` | Yes | HyperShift HostedCluster configuration (see below). |

The `hostedCluster` field contains the full HostedCluster spec. Some fields (e.g. `platform`, `networking`, `etcd`, `services`) are internal — they exist in the CRD for Kubernetes validation but are excluded from the REST API's OpenAPI spec via the `openapi:"hidden"` struct tag mechanism.

**Example:**

```yaml
apiVersion: hcp.gcp.hypershift.openshift.com/v1alpha1
kind: ManagedHostedCluster
metadata:
  name: my-cluster
  namespace: clusters
spec:
  clusterID: my-cluster-001
  versionStreamRef:
    name: stable
  hostedCluster:
    release:
      image: "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"
    infraID: my-cluster-001
    platform:
      type: None
    etcd:
      managementType: Managed
      managed:
        storage:
          type: PersistentVolume
    services:
      - service: APIServer
        servicePublishingStrategy:
          type: LoadBalancer
      - service: OAuthServer
        servicePublishingStrategy:
          type: Route
      - service: Konnectivity
        servicePublishingStrategy:
          type: Route
      - service: Ignition
        servicePublishingStrategy:
          type: Route
    networking:
      clusterNetwork:
        - cidr: 10.132.0.0/14
      serviceNetwork:
        - cidr: 172.31.0.0/16
```

### VersionStream

**Scope:** Cluster

Tracks the target OCP version for a group of clusters. The VersionStream controller queries the [Cincinnati update service](https://api.openshift.com/api/upgrades_info/v1/graph) to resolve the target major.minor version to the latest available patch release and its image pullspec. The resolved image is stored in `status.releaseImage` and used by the HostedCluster controller to override the release image in the ManagedHostedCluster's embedded spec. When `targetVersion` changes, all ManagedHostedClusters referencing this stream are reconciled (subject to their UpgradeSchedule constraints).

The controller re-checks Cincinnati every 5 minutes on success (to pick up new patch releases) and every 1 minute on failure. If Cincinnati becomes unreachable, the last successfully resolved image is preserved so downstream controllers continue to function.

**Spec:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `targetVersion` | `string` | Yes | | Desired OCP version as major.minor (e.g. `"4.16"`). |
| `channelGroup` | `string` | No | `"stable"` | Cincinnati channel group (`"stable"`, `"fast"`, `"candidate"`). |
| `arch` | `string` | No | `"amd64"` | CPU architecture for the release image. |

**Status:**

| Field | Type | Description |
|-------|------|-------------|
| `releaseImage` | `string` | Fully-qualified pullspec resolved from Cincinnati (e.g. `quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64`). |
| `resolvedVersion` | `string` | Exact OCP version resolved (e.g. `"4.16.3"`), representing the latest patch for the target major.minor. |
| `channel` | `string` | Cincinnati channel used for resolution (e.g. `"stable-4.16"`). |
| `conditions` | `[]Condition` | `ImageResolved=True` when the image is successfully resolved; `ImageResolved=False` on failure. |

**Example:**

```yaml
apiVersion: hcp.gcp.hypershift.openshift.com/v1alpha1
kind: VersionStream
metadata:
  name: stable
spec:
  targetVersion: "4.16"
  channelGroup: "stable"
  arch: "amd64"
```

After reconciliation, `kubectl get versionstreams` shows the resolved state:

```
NAME     TARGET   RESOLVED   IMAGE RESOLVED
stable   4.16     4.16.3     True
```

### UpgradeSchedule

**Scope:** Namespaced

Defines when version upgrades are allowed for a cluster. Referenced by ManagedHostedCluster. Spec fields are placeholder -- scheduling semantics (maintenance windows, cron, blackout periods) will be defined in a future iteration.

**Example:**

```yaml
apiVersion: hcp.gcp.hypershift.openshift.com/v1alpha1
kind: UpgradeSchedule
metadata:
  name: weekday-maintenance
  namespace: clusters
spec: {}
```

---

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.25+ | [go.dev/dl](https://go.dev/dl/) |
| Docker or Podman | | [docker.com](https://docs.docker.com/get-docker/) |
| kubectl | 1.35+ | [kubernetes.io](https://kubernetes.io/docs/tasks/tools/) |
| kubebuilder | 4.x | `brew install kubebuilder` |
| A Kubernetes cluster | 1.35+ | [kind](https://kind.sigs.k8s.io/) for local development |

---

## Getting Started

### Build

```bash
make build
```

This runs code generation, formatting, vetting, and compiles the manager binary to `bin/manager`.

### Run Locally

Run the controller against your current kubeconfig context (CRDs must be installed first):

```bash
make install   # install CRDs
make run       # start the controller
```

### Deploy to a Cluster

```bash
# Build and push the container image
make docker-build docker-push IMG=ghcr.io/gcp-hcp/gcp-hcp-backend:latest

# Deploy the controller, RBAC, and CRDs
make deploy IMG=ghcr.io/gcp-hcp/gcp-hcp-backend:latest
```

This deploys into the `gcp-hcp-backend-system` namespace with:
- A `Deployment` running the manager (1 replica, leader election enabled)
- A `ServiceAccount` with least-privilege RBAC
- CRDs for all managed resources
- A metrics `Service` on port 8443 (HTTPS)

To remove everything:

```bash
make undeploy
```

### Create a Managed Cluster

Apply the samples in order -- VersionStream first, then the ManagedHostedCluster:

```bash
# 1. Create a VersionStream (cluster-scoped)
kubectl apply -f config/samples/hcp_v1alpha1_versionstream.yaml

# 2. Create a ManagedHostedCluster (namespaced)
kubectl apply -f config/samples/hcp_v1alpha1_managedhostedcluster.yaml
```

The VersionStream controller will query Cincinnati to resolve the target version to a release image. Once resolved, the HostedCluster controller will build a HyperShift HostedCluster CR from the embedded `.spec.hostedCluster` configuration, override the release image with the Cincinnati-resolved value, and create the CR.

---

## REST API Server

The API server (`cmd/apiserver/`) exposes `ManagedHostedCluster` resources over HTTP. Every request is proxied to the Kubernetes API -- creating a cluster via REST creates the corresponding `ManagedHostedCluster` CR, which the operator's controllers then reconcile.

### Running the API Server

The API server uses your current kubeconfig context. CRDs must be installed first.

```bash
make install        # install CRDs (if not already deployed)
make run-apiserver  # starts the API server on :8080
```

Or build and run the binary directly:

```bash
make build-apiserver
./bin/apiserver -addr=:8080
```

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1alpha1/namespaces/{namespace}/managedhostedclusters` | List all clusters in a namespace |
| `POST` | `/v1alpha1/namespaces/{namespace}/managedhostedclusters` | Create a cluster |
| `GET` | `/v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}` | Get a cluster |
| `PUT` | `/v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}` | Update a cluster (requires `resourceVersion`) |
| `DELETE` | `/v1alpha1/namespaces/{namespace}/managedhostedclusters/{name}` | Delete a cluster |
| `GET` | `/openapi.yaml` | OpenAPI 3.0 spec |
| `GET` | `/healthz` | Health check |

Error responses use `{"error": "message", "code": 404}` format. Kubernetes API errors are mapped to HTTP status codes (404 Not Found, 409 Conflict, 422 Unprocessable Entity, etc.).

### Examples

**Create a cluster:**

```bash
curl -X POST http://localhost:8080/v1alpha1/namespaces/default/managedhostedclusters \
  -H "Content-Type: application/json" \
  -d '{
    "metadata": {"name": "my-cluster"},
    "spec": {
      "clusterID": "cluster-001",
      "versionStreamRef": {"name": "stable"},
      "hostedCluster": {
        "release": {"image": "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"},
        "infraID": "cluster-001"
      }
    }
  }'
```

**List clusters:**

```bash
curl http://localhost:8080/v1alpha1/namespaces/default/managedhostedclusters
```

**Get a specific cluster:**

```bash
curl http://localhost:8080/v1alpha1/namespaces/default/managedhostedclusters/my-cluster
```

**Delete a cluster:**

```bash
curl -X DELETE http://localhost:8080/v1alpha1/namespaces/default/managedhostedclusters/my-cluster
```

---

## OpenAPI Spec Generation

A standalone OpenAPI 3.0 spec is generated from the CRD schemas, suitable for REST API documentation, validation, and client SDK generation. The Go types in `api/v1alpha1/` remain the single source of truth -- the spec is derived from the generated CRDs.

```bash
make openapi
```

This runs `make manifests` first (to regenerate CRD YAMLs from the Go types), then extracts the `openAPIV3Schema` from each CRD and assembles them into a complete OpenAPI 3.0.3 document at `api/openapi/spec.yaml`.

**Field visibility control:** Fields tagged with `openapi:"hidden"` in Go struct definitions are excluded from the generated OpenAPI spec while remaining in the CRD (so Kubernetes validates the full object). The `hack/crd-to-openapi` tool parses Go source files with `go/ast` to discover hidden fields and strips them during spec generation. This allows internal fields (e.g. `platform`, `networking`, `etcd`, `services`) to be set by controllers without exposing them to REST API consumers.

The generated spec includes:
- Schemas for exposed resource types with full validation rules (required fields, patterns, enums, defaults)
- RESTful CRUD paths, respecting Namespaced vs Cluster scope
- Status sub-resource endpoints for types with status subresources
- Synthetic list schemas for collection endpoints

By default, only `ManagedHostedCluster` is included (controlled by the `-kinds` flag in the Makefile). To expose additional CRDs, update the `-kinds` argument in the `openapi` Makefile target.

**Keeping in sync:** After editing any `*_types.go` file, run `make openapi` to regenerate. In CI, `make verify-openapi` checks that the spec is up to date.

**Embedding in Go:** The `api/openapi` package provides the spec via `go:embed`:

```go
import "github.com/gcp-hcp/gcp-hcp-backend/api/openapi"

// openapi.Spec contains the raw YAML bytes of the OpenAPI spec
```

---

## Development

### Project Structure

```
gcp-hcp-backend/
├── api/
│   ├── v1alpha1/                           # CRD type definitions
│   │   ├── groupversion_info.go            # API group registration
│   │   ├── managedhostedcluster_types.go   # ManagedHostedCluster
│   │   ├── hypershift_types.go            # Vendored HyperShift HostedClusterSpec types
│   │   ├── versionstream_types.go          # VersionStream
│   │   ├── upgradeschedule_types.go        # UpgradeSchedule
│   │   └── zz_generated.deepcopy.go       # auto-generated (do not edit)
│   └── openapi/
│       ├── embed.go                        # go:embed wrapper for spec.yaml
│       └── spec.yaml                       # Generated OpenAPI 3.0 spec (do not edit)
├── cmd/
│   ├── main.go                             # Manager entry point (controller)
│   └── apiserver/main.go                   # REST API server entry point
├── hack/
│   └── crd-to-openapi/main.go             # CRD-to-OpenAPI extraction tool
├── internal/
│   ├── apiserver/                          # REST API server
│   │   ├── handlers.go                     # CRUD handlers for ManagedHostedCluster
│   │   └── errors.go                       # K8s error → HTTP status mapping
│   ├── cincinnati/                         # Cincinnati update service client
│   │   ├── client.go                       # HTTP client, VersionResolver interface
│   │   └── client_test.go                  # httptest-based tests
│   └── controller/
│       ├── managedhostedcluster_controller.go  # ManagedHostedCluster reconciler (scaffold)
│       ├── hostedcluster_controller.go         # HostedCluster spec-to-CR controller
│       ├── versionstream_controller.go         # Cincinnati version resolution controller
│       ├── upgradeschedule_controller.go       # UpgradeSchedule reconciler (scaffold)
│       └── suite_test.go                       # envtest setup
├── config/
│   ├── crd/bases/                          # Generated CRD YAML (4 CRDs)
│   ├── rbac/                               # Generated RBAC manifests
│   ├── manager/                            # Manager Deployment
│   ├── default/                            # Kustomize overlay
│   ├── samples/                            # Example CRs for all 4 types
│   └── prometheus/                         # ServiceMonitor (optional)
├── test/e2e/                               # End-to-end tests (Kind cluster)
├── Dockerfile                              # Multi-stage build (distroless runtime)
├── Makefile                                # Build automation
└── PROJECT                                 # Kubebuilder metadata
```

### Make Targets

#### Development

| Target | Description |
|--------|-------------|
| `make manifests` | Generate CRD YAML and RBAC from kubebuilder markers |
| `make generate` | Generate `DeepCopy` methods for API types |
| `make openapi` | Generate OpenAPI 3.0 spec from CRD schemas (`api/openapi/spec.yaml`) |
| `make verify-openapi` | Verify the OpenAPI spec is up to date (for CI) |
| `make fmt` | Run `go fmt` |
| `make vet` | Run `go vet` |
| `make lint` | Run golangci-lint |
| `make lint-fix` | Auto-fix lint issues |

#### Build

| Target | Description |
|--------|-------------|
| `make build` | Compile the manager binary to `bin/manager` |
| `make build-apiserver` | Compile the API server binary to `bin/apiserver` |
| `make run` | Run the controller locally against your kubeconfig |
| `make run-apiserver` | Run the API server locally against your kubeconfig |
| `make docker-build IMG=<image>` | Build the container image |
| `make docker-push IMG=<image>` | Push the container image |
| `make docker-buildx IMG=<image>` | Multi-platform build (arm64, amd64, s390x, ppc64le) |

#### Testing

| Target | Description |
|--------|-------------|
| `make test` | Run unit and integration tests with envtest |
| `make setup-test-e2e` | Create a Kind cluster for e2e tests |
| `make test-e2e` | Run end-to-end tests on the Kind cluster |
| `make cleanup-test-e2e` | Delete the Kind cluster |

#### Deployment

| Target | Description |
|--------|-------------|
| `make install` | Install CRDs only |
| `make uninstall` | Remove CRDs |
| `make deploy IMG=<image>` | Deploy everything (CRDs, RBAC, controller) |
| `make undeploy` | Remove everything |
| `make build-installer` | Generate a single `dist/install.yaml` |

### Code Generation

After modifying any `*_types.go` file, always regenerate:

```bash
make generate    # regenerates zz_generated.deepcopy.go
make manifests   # regenerates CRD YAML and RBAC
make openapi     # regenerates api/openapi/spec.yaml from CRDs
```

`make generate` and `make manifests` are also run automatically as part of `make build` and `make test`. `make openapi` chains from `make manifests` so it always starts with up-to-date CRDs.

**Important:** Do not edit `zz_generated.deepcopy.go`, files under `config/crd/bases/`, or `api/openapi/spec.yaml` by hand. They are overwritten on every generation.

### Adding a New CRD and Controller

```bash
kubebuilder create api \
  --group hcp \
  --version v1alpha1 \
  --kind YourNewKind \
  --resource --controller
```

This scaffolds:
- `api/v1alpha1/yournewkind_types.go` -- define your spec and status here
- `internal/controller/yournewkind_controller.go` -- implement reconciliation here
- Auto-wires the new controller in `cmd/main.go`

Then set concurrent reconciliation in the controller's `SetupWithManager`:

```go
func (r *YourNewKindReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&hcpv1alpha1.YourNewKind{}).
        WithOptions(controller.Options{
            MaxConcurrentReconciles: 10,
        }).
        Complete(r)
}
```

Run `make generate manifests` and you're done.

### Upgrading HyperShift Compatibility

The operator vendors a curated subset of HyperShift types in `api/v1alpha1/hypershift_types.go` rather than importing the upstream Go module. HostedCluster CRs are built by marshaling the typed spec to an unstructured object. To support new upstream fields:

1. Add the new fields to the vendored types in `hypershift_types.go`
2. Annotate internal fields with `openapi:"hidden"` to exclude them from the REST API
3. Run `make generate manifests openapi` to regenerate CRDs and OpenAPI spec
4. Update the `VersionStream` target to point to the new major.minor version
5. The VersionStream controller will automatically resolve the latest patch release and its image from Cincinnati

---

## Testing

### Unit Tests

Unit tests use [envtest](https://book.kubebuilder.io/reference/envtest) -- a real Kubernetes API server and etcd process, no mocking.

```bash
make test
```

Tests are located in `internal/controller/` and use the [Ginkgo](https://onsi.github.io/ginkgo/) v2 framework with [Gomega](https://onsi.github.io/gomega/) matchers.

The test environment:
- Boots a local kube-apiserver + etcd from binaries in `bin/k8s/`
- Registers CRDs from `config/crd/bases/`
- Creates a real Kubernetes client for assertions

### End-to-End Tests

E2e tests run on an isolated [Kind](https://kind.sigs.k8s.io/) cluster.

```bash
make setup-test-e2e   # creates the Kind cluster
make test-e2e         # builds, deploys, and tests
make cleanup-test-e2e # tears down the cluster
```

The e2e suite:
- Builds the manager image and loads it into Kind
- Deploys the operator with restricted pod security
- Validates the controller pod reaches `Running` state
- Verifies the HTTPS metrics endpoint returns 200

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `KIND_CLUSTER` | `gcp-hcp-backend-test-e2e` | Kind cluster name |
| `CERT_MANAGER_INSTALL_SKIP` | `false` | Skip cert-manager installation |

---

## Configuration

### API Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:8080` | HTTP listen address |

The API server uses the current kubeconfig context (or in-cluster config when deployed inside Kubernetes).

### Manager Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--leader-elect` | `false` | Enable leader election for HA |
| `--health-probe-bind-address` | `:8081` | Health and readiness probe address |
| `--metrics-bind-address` | `0` (disabled) | Metrics bind address (`:8443` for HTTPS, `:8080` for HTTP) |
| `--metrics-secure` | `true` | Serve metrics over HTTPS |
| `--enable-http2` | `false` | Enable HTTP/2 (disabled by default for CVE mitigation) |
| `--metrics-cert-path` | | Custom TLS cert directory for metrics |
| `--webhook-cert-path` | | Custom TLS cert directory for webhooks |

### Metrics

When deployed, the metrics endpoint is exposed at `https://<service>:8443/metrics` with authentication via Kubernetes TokenReview and SubjectAccessReview. The `metrics-reader` ClusterRole grants read access.

### Leader Election

Enabled by default in the deployed manifest (`--leader-elect` flag). Uses Kubernetes Lease objects in the operator's namespace. Required when running multiple replicas.

---

## Security

The operator follows Kubernetes security best practices:

- **Non-root execution** -- runs as UID 65532 (distroless `nonroot` user)
- **Read-only filesystem** -- `readOnlyRootFilesystem: true`
- **No privilege escalation** -- `allowPrivilegeEscalation: false`
- **Dropped capabilities** -- all Linux capabilities dropped
- **Seccomp** -- `RuntimeDefault` profile
- **Restricted pod security** -- namespace labeled with `pod-security.kubernetes.io/enforce=restricted`
- **Metrics auth** -- HTTPS with TokenReview/SubjectAccessReview
- **HTTP/2 disabled** -- mitigates CVE-2023-44487 and related vulnerabilities
- **Minimal base image** -- `gcr.io/distroless/static:nonroot` (no shell, no package manager)

---

## Project Distribution

### YAML Bundle

Generate a single manifest containing all resources:

```bash
make build-installer IMG=ghcr.io/gcp-hcp/gcp-hcp-backend:latest
```

Then install with:

```bash
kubectl apply -f dist/install.yaml
```

### Helm Chart

Optionally generate a Helm chart:

```bash
kubebuilder edit --plugins=helm/v2-alpha
```

The chart is generated under `dist/chart/`.

---

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

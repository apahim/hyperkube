# Firestore + kube-applier Integration Plan

## Overview

Replace the API server's direct Kubernetes client with Firestore-based desire
writes. A separate kube-applier agent (already implemented in
`gcp-hcp/experiments/kube-applier-gcp`) watches Firestore and reconciles
desires into the Kubernetes API. The API server becomes a thin Firestore writer
with no Kubernetes dependency.

Additionally, introduce a placement layer so the API server can resolve which
management cluster should host a new cluster, using a global Firestore database
with atomic capacity tracking.

## Current State

The API server (`cmd/apiserver/main.go`) creates a `controller-runtime`
`client.Client` at startup and the `Handler` struct (`internal/apiserver/handlers.go`)
uses it directly for all CRUD operations against `ManagedHostedCluster` CRs.

## Target Architecture

```
                                    Firestore
                              ┌───────────────────┐
                              │  placement-db      │
                              │  └─ mgmtclusters/  │
                              │     mc-01: 37/100  │
                              │     mc-02: 99/100  │
                              └────────┬───────────┘
                                       │
  HTTP ──► API Server ─────────────────┼──────────────────┐
           (Firestore only,            │                  │
            no K8s client)             ▼                  ▼
                              ┌─────────────────┐ ┌─────────────────┐
                              │ mc-01-specs      │ │ mc-02-specs      │
                              │  applydesires/   │ │  applydesires/   │
                              │  readdesires/    │ │  readdesires/    │
                              │  deletedesires/  │ │  deletedesires/  │
                              └────────┬─────────┘ └────────┬─────────┘
                                       │                    │
                              ┌────────┴─────────┐ ┌───────┴──────────┐
                              │ kube-applier      │ │ kube-applier      │
                              │ agent (mc-01)     │ │ agent (mc-02)     │
                              │  ▼ K8s API        │ │  ▼ K8s API        │
                              └──────────────────┘ └──────────────────┘
                                       │                    │
                              ┌────────┴─────────┐ ┌───────┴──────────┐
                              │ mc-01-status      │ │ mc-02-status      │
                              │  applydesires/    │ │  applydesires/    │
                              │  readdesires/     │ │  readdesires/     │
                              │  deletedesires/   │ │  deletedesires/   │
                              └──────────────────┘ └──────────────────┘
```

---

## Part 1: Placement Layer

### 1.1 Global Firestore Database

Create a Firestore named database (e.g. `placement`) that holds
cluster-global state not scoped to any single management cluster.

### 1.2 Management Cluster Collection

A `managementclusters` collection with one document per management cluster:

```json
// managementclusters/mc-01
{
  "capacity": 100,
  "allocated": 37
}
```

All management clusters are in a single region so no region field is needed.

### 1.3 Placement Decision Flow

On a create request:

1. Start a Firestore transaction on the `placement` database.
2. Query `managementclusters` where `allocated < capacity`.
3. Pick a cluster (e.g. lowest `allocated`).
4. Atomically increment `allocated` by 1 and commit.
5. Use the chosen management cluster name to derive the specs database name
   (`mc-{name}-specs`) for writing desires.

On delete: run a transaction to decrement `allocated` by 1 for the management
cluster that owns the cluster being deleted. The management cluster name is
stored on the ApplyDesire's `spec.managementCluster` field, so the delete
handler looks up the existing ApplyDesire first to find it.

### 1.4 Namespace-to-Cluster Model

Each namespace maps to a customer project. The common case is one cluster
per project, but there is no hard limit — a project may have multiple
clusters.

- The `clustermap` collection (see 2.8) is keyed by `{namespace}/{name}`
  to support multiple clusters per namespace.
- **List (common case):** When a namespace has one cluster on one MC, List
  is a single clustermap prefix query + single status database query.
- **List (multi-cluster):** When a namespace has clusters on different MCs,
  List queries the clustermap by namespace prefix, groups results by MC,
  and fans out to each MC's status database. The results are merged before
  returning to the client.

### 1.5 Implementation Tasks

- [x] Add `cloud.google.com/go/firestore` dependency to `go.mod`.
- [x] Create a `internal/placement` package with:
  - A `Client` struct holding a Firestore client for the `placement` database.
  - `Allocate(ctx) (managementCluster string, err error)` — runs the
    transaction described above, returns the chosen management cluster name.
  - `Release(ctx, managementCluster string) error` — decrements `allocated`
    in a transaction.
- [x] Add `--gcp-project`, `--placement-database`, and `--cedar-database`
  flags to the API server.
- [x] Initialize the placement client in `cmd/apiserver/main.go` and pass it
  to the handler.

---

## Part 2: Firestore Desire Writes (Replace Kubernetes Client)

### 2.1 Dependency on kube-applier

**Decision: Option C — reimplement against the schema contract.**

The kube-applier code lives in `gcp-hcp/experiments/kube-applier-gcp`. Rather
than importing an unstable module or copying code, this repo reimplements
the thin Firestore CRUD layer using the same Firestore collections and
document schema. The schema is the contract between the API server and the
kube-applier agent.

The required pieces to reimplement are:
- Desire types: `ApplyDesire`, `DeleteDesire`, `ReadDesire`,
  `ResourceReference`, `FirestoreMetadata`.
- Firestore CRUD: `ResourceCRUD[T]` and `KubeApplierDBClient` with the
  `applydesires`, `deletedesires`, `readdesires` collections.
- Document ID generation: `desireid.NewDocumentID()` (UUID v5).

### 2.2 Handler Changes

The `Handler` struct currently holds `client.Client`. Replace it with:

```go
type Handler struct {
    Placement  *placement.Client
    SpecsDB    func(managementCluster string) database.KubeApplierDBClient
    StatusDB   func(managementCluster string) database.KubeApplierDBClient
}
```

`SpecsDB` and `StatusDB` are factory functions because the target database
depends on the management cluster, which is only known after placement (for
create) or after looking up the existing desire (for get/update/delete).

Alternatively, cache Firestore clients per management cluster in a
thread-safe map to avoid creating a new client on every request.

### 2.3 Handler: Create (POST)

Current: decode JSON, set namespace/TypeMeta, `h.Client.Create()`.

New:

1. Decode JSON into `ManagedHostedCluster`.
2. Set namespace and TypeMeta. Serialize the full CR to JSON (this becomes
   `kubeContent`).
3. Call `h.Placement.Allocate(ctx)` to get the management cluster.
4. Build the `ResourceReference` for the CR:
   - Group: `hcp.gcp.hypershift.openshift.com`
   - Version: `v1alpha1`
   - Resource: `managedhostedclusters`
   - Namespace: from URL path
   - Name: from the decoded object
5. Compute the document ID via `desireid.NewDocumentID(taskKey, ...)`.
6. Write an `ApplyDesire` to `mc-{cluster}-specs` / `applydesires` with
   `kubeContent` set to the serialized CR.
7. Write a `ReadDesire` to `mc-{cluster}-specs` / `readdesires` with the
   same `ResourceReference` (so the agent immediately starts watching the
   object and mirroring its status).
8. Return 200 with the object as submitted (not the K8s-applied version,
   since apply is async).

**Async semantics:** The 200 response confirms the desire was written to
Firestore. It does not mean the resource exists in Kubernetes yet. Clients
(e.g. the CLI) must poll GET requests to learn the actual status of the
cluster after creation. The GET response will include the kube-applier's
sync status — see section 2.4.

**No rollback on partial failure:** If placement succeeds (allocated
incremented) but the ApplyDesire write fails, the placement counter is
orphaned. This is acceptable for now — the counter can be corrected
manually or by a periodic reconciliation job in a future iteration.

### 2.4 Handler: Get (GET by name)

Current: `h.Client.Get()`.

New:

1. Compute the document ID from the URL path parameters.
2. Look up the `ReadDesire` from the status database. The management cluster
   name is needed to find the right database; derive it from the ApplyDesire
   in the specs database (or from a mapping/index).
3. Return `status.kubeContent` as the response (the live K8s object mirrored
   by the kube-applier agent).
4. If `kubeContent` is nil (agent hasn't synced yet), return the object with
   a status indicating it's pending.

### 2.5 Handler: List (GET collection)

Current: `h.Client.List()`.

New:

1. Query the `clustermap` collection by namespace prefix to find all
   entries for the namespace (e.g. `{namespace}/*`).
2. Group the results by management cluster.
3. For each MC, list `ReadDesire` documents from that MC's status database
   filtered by the resource type and namespace.
4. Collect `status.kubeContent` from each, merge across MCs.
5. Return as a list.

In the common case (one cluster per project), this reduces to a single
clustermap lookup and a single status database query. Fan-out only occurs
when a project has clusters spread across multiple MCs.

### 2.6 Handler: Update (PUT)

Current: decode JSON, `h.Client.Update()`.

New:

1. Decode JSON into `ManagedHostedCluster`.
2. Look up the existing `ApplyDesire` to find the management cluster.
3. Update `spec.kubeContent` on the existing `ApplyDesire` with the new
   serialized CR (using `Replace()` for optimistic concurrency).
4. Return 200.

### 2.7 Handler: Delete (DELETE)

Current: `h.Client.Delete()`.

New:

1. Look up the existing `ApplyDesire` to find the management cluster.
2. Delete the `ApplyDesire` from the specs database.
3. Delete the `ReadDesire` from the specs database.
4. Write a `DeleteDesire` to the specs database.
5. Call `h.Placement.Release(ctx, managementCluster)` to decrement the
   counter.
6. Delete the `{namespace}/{name}` entry from the `clustermap` collection.
7. Return 200.

Order matters: remove ApplyDesire before writing DeleteDesire, otherwise the
apply controller would re-create the object after the delete controller
removes it.

**Status database cleanup:** The kube-applier agent is responsible for
cleaning up its own entries in the status database. When a DeleteDesire is
processed, the agent deletes the Kubernetes resource and then removes the
corresponding ApplyDesire and ReadDesire documents from the status database.
The API server does not write to or clean up the status database directly.

### 2.8 Resolving the Management Cluster for Existing Resources

For get/update/delete, the handler needs to know which management cluster
owns a given resource.

**Decision: Option A — clustermap collection.**

A `clustermap` collection in the `placement` database provides O(1) lookup.
Each document is keyed by `{namespace}/{name}` to support multiple clusters
per namespace:

```json
// clustermap/my-project-namespace/my-cluster
{
  "managementCluster": "mc-01"
}
```

Written at create time, deleted at delete time (see 2.7 step 6). For List,
query by namespace prefix to find all clusters in a project (see 2.5).

### 2.9 Implementation Tasks

- [x] Reimplement kube-applier desire types and Firestore CRUD in
  `internal/desires/` using the kube-applier schema as the contract (2.1).
- [x] Implement `desireid` document ID generation (UUID v5) in
  `internal/desires/`.
- [x] Add a `clustermap` collection to the placement database for MC lookup
  (2.8).
- [x] Rewrite `Handler` struct to hold placement + Firestore dependencies
  instead of `client.Client`.
- [x] Rewrite `Create` handler (2.3).
- [x] Rewrite `Get` handler (2.4).
- [x] Rewrite `List` handler (2.5).
- [x] Rewrite `Update` handler (2.6).
- [x] Rewrite `Delete` handler (2.7).
- [x] Remove the Kubernetes client from `Handler` struct and handler init
  in `cmd/apiserver/main.go` (K8s client retained only for Cedar).
- [x] Remove `controller-runtime` and `client-go` dependencies from the API
  server binary (they remain in the controller manager binary).
- [ ] Update the OpenAPI spec if response semantics change (e.g. 200 instead
  of 201 for create).

---

## Part 3: What Changes, What Stays

- **OpenAPI validation middleware** (`internal/apiserver/validation.go`):
  unchanged. Still validates request bodies before they reach handlers.
- **Controller manager** (`cmd/main.go`): unchanged. It still watches CRs in
  K8s (created by the kube-applier agent) and reconciles them.
- **CRD types** (`api/v1alpha1/`): unchanged. The types define the schema;
  the serialized CR is passed as `kubeContent` in the ApplyDesire.
- **Cedar authorization middleware** (`internal/cedar/`): moves entirely to
  Firestore. See Part 5 below.

---

## Part 4: Considerations

### Error Reporting

Apply failures are reported asynchronously via `Successful`/`Degraded`
conditions on the ApplyDesire in the status database. The API server can
expose these on the GET response so clients know if the apply succeeded.
Clients poll GET requests to learn the outcome of async operations.

### taskKey

`desireid.NewDocumentID()` takes a `taskKey` as the first input. This
differentiates document IDs for the same K8s resource when managed by
different field managers. Define a constant `taskKey` for the API server
(e.g. `"hyperkube-apiserver"`).

### Client Caching

The API server will need Firestore clients for `placement` (one, at
startup), for the `cedar` database (one, at startup), and for each
management cluster's specs/status databases (up to 2 * N_mc clients).

Use a bounded `sync.Map` with a maximum size (e.g. 2 * max expected
management clusters). Entries should be evicted on an LRU basis or when a
management cluster is decommissioned. On startup, only the `placement` and
`cedar` clients are created; per-MC clients are created lazily on first
request and cached thereafter.

---

## Part 5: Cedar Migration to Firestore

### 5.1 Overview

The Cedar authorization middleware (`internal/cedar/`) currently depends on
a Kubernetes client for all its storage. To fully remove the K8s dependency
from the API server, Cedar must move to Firestore.

The Cedar middleware has three K8s dependencies:

1. **Resource labels for per-resource authz** (`authz.go:66-74`): fetches
   `ManagedHostedCluster` labels from K8s to pass as `ResourceAttributes`
   to Cedar policy evaluation.
2. **Policy attachments** (`store.go`): stores Cedar policy attachments as
   JSON in ConfigMaps (`cedar-attachments-{projectID}`).
3. **Global policies** (`store.go`): reads global Cedar policies from a
   ConfigMap (`cedar-global-policies`).
4. **CustomRole CRs** (`store.go`): reads `CustomRole` custom resources
   to resolve custom role permissions and generate Cedar policy text.

All four must move to Firestore.

### 5.2 Cedar Firestore Database

Create a Firestore named database (e.g. `cedar`) in the same GCP project
as the placement database.

Collections:

```
cedar-db/
├── attachments/
│   └── {projectID}/          # one document per project
│       {
│         "attachments": [...]  # same JSON structure as today
│       }
├── global-policies/
│   └── default/              # single document
│       {
│         "policies": "..."   # Cedar policy text
│       }
└── custom-roles/
    └── {projectID}:{roleName}/
        {
          "permissions": [...],
          "description": "...",
          "conditions": [...]
        }
```

### 5.3 Resource Labels

Replace the K8s `Get` call in `authz.go:66-74` with a Firestore read. The
resource labels are available from the `ReadDesire`'s `status.kubeContent`
in the management cluster's status database. The authz middleware reads the
`kubeContent`, deserializes the `ManagedHostedCluster`, and extracts its
labels.

This requires the authz middleware to have access to the `clustermap`
(to find the MC) and the status database client factory (to read the
ReadDesire). Pass these as dependencies when constructing the middleware.

### 5.4 Store Migration

Replace `Store.client` (`client.Client`) with a Firestore client for the
`cedar` database. The method signatures stay the same; only the backing
storage changes:

| Current (K8s)                                  | New (Firestore)                              |
|------------------------------------------------|----------------------------------------------|
| `getAttachments` reads ConfigMap data key       | Reads `attachments/{projectID}` document      |
| `saveAttachments` creates/updates ConfigMap     | Writes `attachments/{projectID}` document     |
| `loadGlobalPolicies` reads ConfigMap            | Reads `global-policies/default` document      |
| `validateCustomRole` gets CustomRole CR         | Reads `custom-roles/{projectID}:{roleName}`   |
| `resolveAttachmentPolicy` gets CustomRole CR    | Reads `custom-roles/{projectID}:{roleName}`   |
| `ListRoles` lists CustomRole CRs in namespace  | Queries `custom-roles/` by projectID prefix   |
| `GetRole` gets CustomRole CR by name            | Reads `custom-roles/{projectID}:{roleName}`   |

### 5.5 User-to-Projects Reverse Index

Cedar attachments record "user X has role Y on project Z," but there is no
efficient way to answer "which projects does user X have access to?" This
is needed for multi-tenancy: when a user lists their clusters, the API must
first discover which projects (namespaces) they can see.

Add a `user-projects` collection to the `cedar` Firestore database:

```json
// user-projects/{userID}
{
  "projects": ["project-alpha", "project-beta"]
}
```

**Write path:** Whenever an attachment is created or deleted, update the
corresponding `user-projects/{userID}` document:
- On `CreateAttachment`: add the `projectID` to the user's `projects`
  list (if not already present).
- On `DeleteAttachment`: if the user has no remaining attachments for that
  project, remove the `projectID` from their `projects` list.

Both operations should run in a Firestore transaction alongside the
attachment write to keep the index consistent.

**Read path:** A "list my projects" API endpoint (or an internal helper
used by the List handler) reads `user-projects/{userID}` to get the set
of projects the authenticated user can access. The handler then queries
each project's clusters via the clustermap.

**Consistency:** The reverse index is eventually consistent with
attachments only if both writes happen in the same transaction. Since
attachments already live in Firestore (after the migration), this is
straightforward.

### 5.6 Implementation Tasks

- [x] Add `--cedar-database` flag to the API server (was already added in
  Phase 2; `--cedar-project` derived from `--gcp-project`).
- [x] Create a Firestore client for the `cedar` database in
  `cmd/apiserver/main.go`.
- [x] Rewrite `Store` to use Firestore instead of ConfigMaps for
  attachments, global policies, and custom roles.
- [x] Add the `user-projects` reverse index collection and update
  `CreateAttachment`/`DeleteAttachment` to maintain it transactionally.
- [x] Add a `ListUserProjects` internal helper that reads
  `user-projects/{userID}`.
- [x] Update `AuthzMiddleware` to read resource labels from the ReadDesire
  status database instead of K8s.
- [x] Pass clustermap + status DB factory to `AuthzMiddleware`.
- [x] Remove the `client.Client` field from `Store` and `AuthzMiddleware`.
- ~~Provide a migration path~~ — not needed; no existing data to migrate.

---

## Implementation Log

### Phase 1: Foundation (completed)

**Delivered:**
- `internal/desires/` package: types, desireid, codec, CRUD layer, errors, DBClient
- `internal/firestorecache/` package: thread-safe client cache
- Updated `internal/apiserver/errors.go` with Firestore error mapping
- Unit tests for desireid (including cross-verification against kube-applier)
  and codec round-trip

**Schema contract verification:**
- Document ID generation matches kube-applier exactly (UUID v5, same namespace
  UUID `a3f1b2c4-d5e6-4f7a-8b9c-0d1e2f3a4b5c`, same input format).
- All desire types match kube-applier field names and `firestore:` tags exactly.
- KubeContent codec uses identical `spec_kubeContent`/`status_kubeContent`
  field names and serialization logic.

**Drift from design doc:** None.

**Design decisions made:**
- `taskKey = "hyperkube-apiserver"` (constant in `internal/desires/desireid.go`).
- Simplified `DBClient` to wrap a single Firestore database (not the
  kube-applier's specs+status pair). The API server creates separate DBClients
  for specs and status databases as needed.
- Client cache uses `sync.Mutex` + `map` (not `sync.Map`) since LRU is not
  needed with <20 management clusters.
- Added `IsAlreadyExistsError` helper (not in kube-applier's errors.go but
  needed for the Create handler's duplicate detection).

### Phase 2: Placement Layer (completed)

**Delivered:**
- `internal/placement/` package: Client with Allocate, Release, and full
  clustermap CRUD (Set/Get/Delete/ListClusterMappings)
- API server flags: `--gcp-project`, `--placement-database`, `--cedar-database`
- Firestore client cache wired into `cmd/apiserver/main.go` with DB factory
  functions (`mc-{name}-specs`, `mc-{name}-status`)
- Handler struct extended with Placement, SpecsDB, StatusDB fields

**Drift from design doc:**
- Clustermap document IDs use `:` separator (`{namespace}:{name}`) instead of
  `/` — Firestore document IDs cannot contain `/` (path separator). Documents
  store `namespace` and `name` as explicit fields to support queries.
- Single `--gcp-project` flag instead of separate `--placement-project`.
- ListClusterMappings queries by `namespace` field equality rather than
  document ID prefix.
- Firestore initialization is optional (gated on `--gcp-project` being set)
  so the API server can still run with K8s client only during transition.

### Phase 3: Handler Rewrites (completed)

**Delivered:**
- All 6 handler methods (Create, Get, List, Update, Delete, GetKubeConfig)
  rewritten to use Firestore via Placement, SpecsDB, and StatusDB
- Added helper functions: `mhcResourceRef`, `mhcDocumentID`, `serializeCR`
- Added package-level constants: `mhcGroup`, `mhcVersion`, `mhcResource`
- Fixed `placement.GetClusterMapping` to return unwrapped gRPC status errors
  so `desires.IsNotFoundError` catches them in `writeError`

**Drift from design doc:**
- `Handler.Client` field retained (removed in Phase 4) — handlers no longer
  use it but the field remains for Cedar middleware which still depends on
  a K8s client passed separately.
- Get handler: when ReadDesire exists in the status DB but `kubeContent` is
  nil (agent hasn't synced), falls back to the ApplyDesire's `spec.kubeContent`
  from the specs DB. The design doc mentioned "return with a status indicating
  pending" — the fallback to spec kubeContent is more useful to clients.
- List handler: filters ReadDesires client-side by
  `spec.TargetItem.Namespace` and cross-references against clustermap names,
  rather than a Firestore query. Sufficient for current scale.
- Create handler returns 200 (not 201) per design doc async semantics.
- `placement.GetClusterMapping` changed from `fmt.Errorf %w` wrapping to
  `status.Errorf` for gRPC NotFound errors — the `%w` wrapping broke
  `status.Code()` extraction in `writeError`.

**Design decisions made:**
- Shared document ID for ApplyDesire and ReadDesire for the same resource
  (same UUID v5 input). This matches the kube-applier's expectation.
- Delete handler fetches the existing ApplyDesire first to extract `ClusterID`
  for the DeleteDesire, before deleting it.

### Phase 4: Remove K8s Client from Handler (completed)

**Delivered:**
- Removed `Client client.Client` field from `Handler` struct
- Removed `controller-runtime/pkg/client` import from `handlers.go`
- Removed `Client: k8sClient` from Handler initialization in `main.go`

**Drift from design doc:**
- `ctrl.GetConfigOrDie()` and `client.New()` are NOT removed yet — they remain
  in `main.go` because Cedar (`Store` and `AuthzMiddleware`) still depends on
  the K8s client. Full removal deferred to Phase 5.
- Scheme registration also retained for Cedar's CustomRole type.

**What remains for Phase 5:**
- `k8sClient` used by `cedar.NewStore()` and `cedar.NewAuthzMiddleware()`
- `ctrl.GetConfigOrDie()`, `client.New()`, scheme init

### Phase 5: Cedar Migration to Firestore (completed)

**Delivered:**
- `Store` rewritten to use Firestore (`cedar` named database) for all storage:
  attachments, global policies, custom roles
- `AuthzMiddleware` rewritten to use `placement.Client` + `statusDB` factory
  for resource label fetch instead of K8s client
- `user-projects` reverse index collection with transactional maintenance
  in `CreateAttachment`/`DeleteAttachment` and `ListUserProjects` helper
- `cmd/apiserver/main.go` fully cleaned: K8s client, scheme registration,
  `ctrl`, `client-go`, and `controller-runtime` imports all removed
- `--gcp-project` is now required (no longer optional gating)
- `--cedar-policy-namespace` and `--cedar-global-policy-configmap` flags removed
  (no longer applicable)
- `errors.go` cleaned: removed K8s `apierrors` mapping (only desires gRPC
  status errors remain)
- All tests updated to use Firestore emulator (`FIRESTORE_EMULATOR_HOST`);
  skip when emulator not available. Pure-logic tests (policy generation,
  entity map, mapper, permissions) still run without emulator.

**Drift from design doc:**
- `NewStore` signature simplified to `NewStore(fsClient *firestore.Client)`
  — removed `namespace` and `globalPolicyConfigMap` params (no longer relevant).
- `NewAuthzMiddleware` signature changed: takes `*placement.Client` and
  `func(string) (*desires.DBClient, error)` instead of `client.Client`.
- `user-projects` index: `addUserProject`/`removeUserProject` are private
  methods called from `CreateAttachment`/`DeleteAttachment`; not exposed as
  a separate API endpoint — `ListUserProjects` is the read-side helper.
- Custom roles Firestore collection uses `{projectID}:{roleName}` document
  IDs with `:` separator (matching clustermap convention).
- `ListRoles` fetches all custom-role documents and filters by projectID
  prefix client-side (no Firestore `Where` query on document ID prefix).
- Migration script removed from scope — no existing data to migrate.
- `k8s.io/apimachinery` remains as a dependency for type definitions
  (`metav1.TypeMeta`, `runtime.RawExtension`) used by the MHC CRD types.
  `controller-runtime` and `client-go` are fully removed from the API server.

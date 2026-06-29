# SandboxSet Substrate Backend Integration — Design Spec

- Date: 2026-06-29
- Branch: `prototype-for-substrate-support`
- Scope: `pkg/controller/sandboxset/`, `pkg/sandbox-manager/infra/substrate/`, `pkg/servers/e2b/`, `cmd/sandbox-manager/`

## 1. Background

OpenKruise Agents currently manages sandbox workloads exclusively through Kubernetes Sandbox CRDs backed by Pods. [Substrate](https://github.com/agent-substrate/substrate) offers an alternative execution model with Actors — lightweight, snapshot-capable execution units that can be paused, suspended, and resumed with low latency via gRPC.

Integrating Substrate as a backend for `SandboxSet` enables:

- Sub-second Actor resume from snapshot (vs. tens of seconds for Pod cold start).
- Efficient hibernation: Actors can be paused (local snapshot, keeps worker) or suspended (external snapshot, frees worker).
- Separation of capacity (WorkerPool) from instance lifecycle (Actor), avoiding per-sandbox CRD overhead.

The challenge is bridging OpenKruise's declarative CRD model with Substrate's imperative gRPC lifecycle without introducing unnecessary CRD reconcile loops in the latency-critical path.

## 2. Goals & Non-Goals

### Goals

- Allow `SandboxSet` to use Substrate as a backend, materializing `ActorTemplate` and `WorkerPool` CRDs instead of `Sandbox` CRs.
- Implement `SubstrateInfra` in sandbox-manager that manages Actor lifecycle via Substrate gRPC, with E2B API compatibility.
- Maintain an in-memory metadata store (`sandboxID -> actorID/owner/route/phase`) to replace Sandbox CRD status.
- Support lazy pooling: only pre-warm WorkerPool and ActorTemplate; Actors are created on demand via E2B create.
- Map `SandboxSet.spec.replicas` directly to `WorkerPool.spec.replicas` (worker pod count).
- Support hibernate modes: `pause` (PauseActor — local snapshot, keeps worker) and `suspend` (SuspendActor — external snapshot, frees worker).

### Non-Goals

- Managing Substrate Actor lifecycle via `Sandbox` / `SandboxClaim` CRDs — Actor lifecycle is imperative-only through sandbox-manager.
- Syncing individual Actor state back to `Sandbox.status`.
- Pre-creating an idle Actor pool (no eager pooling in this version).
- Rolling upgrade of running Actors when `SandboxSet.spec.template` changes.
- Clone, checkpoint, or CSI mount operations on Substrate Actors (this version).
- Decoupling `SandboxSet.spec.replicas` from worker pod count.

## 3. Architecture

```
                    Declarative Plane                          Imperative Plane
              ┌─────────────────────────────┐        ┌──────────────────────────────────┐
              │  SandboxSet CRD             │        │  E2B SDK / REST Client           │
              │       │                     │        │       │                           │
              │       ▼                     │        │       ▼                           │
              │  SandboxSetController       │        │  sandbox-manager                  │
              │       │                     │        │       │                           │
              │  ┌────┴────┐                │        │       ▼                           │
              │  ▼         ▼                │        │  SubstrateInfra                   │
              │ ActorTemplate  WorkerPool   │        │       │                           │
              └─────────────────────────────┘        │       ▼                           │
                                                     │  Substrate Control gRPC           │
                                                     │       │                           │
                                                     │       ▼                           │
                                                     │  Actor ──► WorkerPool Worker Pod  │
                                                     │                                   │
                                                     │  Metadata Store (in-memory)        │
                                                     │  sandbox-gateway (route table)     │
                                                     └──────────────────────────────────┘
```

### 3.1 Two Planes

| Plane | Component | Responsibility |
|---|---|---|
| Declarative Capacity Plane | `SandboxSetController` | Materialize `ActorTemplate` and `WorkerPool` from `SandboxSet`. No Actor lifecycle. |
| Imperative Lifecycle Plane | `sandbox-manager` + `SubstrateInfra` | Handle E2B create/connect/pause/kill via Substrate gRPC. Manage Actor lifecycle directly. |

The `SandboxController` does not participate in Substrate Actor lifecycle. `SandboxClaim` does not allocate Actors from Substrate. This keeps CRD reconcile loops out of the latency-critical lifecycle path.

### 3.2 Core Conclusion

```
SandboxSet              = Substrate backend capacity and template declaration
Sandbox / SandboxClaim  = Not involved in Substrate Actor lifecycle
E2B API / sandbox-manager = The sole imperative entry point for Substrate Actor lifecycle
Substrate WorkerPool    = Actual worker pod pool, replicas == SandboxSet.spec.replicas
Substrate ActorTemplate = Runtime template generated from SandboxSet.spec.template
Substrate Actor         = Lazily created, paused, resumed, deleted via E2B API
```

## 4. Resource Mapping

### 4.1 SandboxSet to Substrate Resources

| OpenKruise `SandboxSet` | Substrate Resource | Notes |
|---|---|---|
| `metadata.name` | `ActorTemplate` / `WorkerPool` name prefix | Stable naming via hash. |
| `metadata.namespace` | Substrate CR namespace | Same namespace as SandboxSet. |
| `spec.template` / `spec.templateRef` | `ActorTemplate` spec | First container image → `pauseImage`; additional containers → `containers`. |
| `spec.replicas` | `WorkerPool.spec.replicas` | 1:1 mapping — represents worker pod count. |
| Hibernate mode annotation | `PauseActor` vs `SuspendActor` | Controls E2B pause behavior. |
| `spec.updateStrategy` | ActorTemplate revision | Template changes generate new ActorTemplate; new Actors use new template. |

### 4.2 Objects Not Mapped

| OpenKruise Object | Handling in Substrate Backend |
|---|---|
| `Sandbox` | Not created. Not used as Actor lifecycle projection. |
| `SandboxClaim` | Not supported. Use E2B create to create Actors directly. |
| `Checkpoint` | Not supported in this version. May map to Substrate snapshot later. |

## 5. Backend Identification

Annotation-based backend selection:

```yaml
apiVersion: agents.kruise.io/v1alpha1
kind: SandboxSet
metadata:
  name: code-interpreter
  namespace: default
  annotations:
    agents.kruise.io/backend: substrate
    substrate.agents.kruise.io/worker-pool-name: code-interpreter
    substrate.agents.kruise.io/hibernate-mode: suspend
    substrate.agents.kruise.io/ateom-image: substrate/ateom-gvisor:latest
    substrate.agents.kruise.io/snapshots-location: gs://my-bucket/snapshots
spec:
  replicas: 10
  template:
    spec:
      containers:
      - name: main
        image: example/code-interpreter@sha256:abc123
```

### 5.1 Annotations

```go
const (
    // AnnotationBackend specifies the backend implementation for a SandboxSet.
    AnnotationBackend = "agents.kruise.io/backend"

    // BackendSubstrate is the annotation value indicating Substrate backend.
    BackendSubstrate = "substrate"

    // AnnotationSubstrateWorkerPoolName overrides the generated WorkerPool name.
    AnnotationSubstrateWorkerPoolName = "substrate.agents.kruise.io/worker-pool-name"

    // AnnotationSubstrateHibernateMode specifies the hibernate mode: "pause" or "suspend".
    AnnotationSubstrateHibernateMode = "substrate.agents.kruise.io/hibernate-mode"

    // AnnotationSubstrateAteomImage specifies the ateom container image for WorkerPool.
    AnnotationSubstrateAteomImage = "substrate.agents.kruise.io/ateom-image"

    // AnnotationSubstrateSnapshotsLocation specifies GCS location for ActorTemplate snapshots.
    AnnotationSubstrateSnapshotsLocation = "substrate.agents.kruise.io/snapshots-location"
)
```

### 5.2 Status Annotations (set by controller)

```go
const (
    // AnnotationStatusActorTemplateName records the current ActorTemplate name.
    AnnotationStatusActorTemplateName = "substrate.agents.kruise.io/actor-template-name"

    // AnnotationStatusWorkerPoolName records the current WorkerPool name.
    AnnotationStatusWorkerPoolName = "substrate.agents.kruise.io/worker-pool-name"
)
```

## 6. SandboxSet Controller Changes

When `SandboxSet` is annotated with `agents.kruise.io/backend=substrate`, the controller bypasses all Sandbox CR management and delegates to `reconcileSubstrate`.

### 6.1 Reconcile Flow

```
1. Watch SandboxSet
2. Check backend annotation → if "substrate", branch to reconcileSubstrate
3. Compute ActorTemplate spec from SandboxSet.spec.template:
   - First container image → ActorTemplate.pauseImage
   - Additional containers → ActorTemplate.containers
   - Annotations → snapshotsLocation, sandboxClass
4. Compute template hash for stable naming: <sandboxset>-<hash[:8]>
5. Create ActorTemplate (idempotent — skip if already exists)
6. Create or update WorkerPool:
   - WorkerPool.spec.replicas = SandboxSet.spec.replicas
   - WorkerPool.spec.ateomImage from annotation
7. Update SandboxSet annotations with resource names (actor-template-name, worker-pool-name)
8. Update SandboxSet.status (updateRevision = hash, replicas)
9. End reconcile
```

### 6.2 Prohibited Behavior

When `backend=substrate`, SandboxSetController must NOT:

- Create `Sandbox` CRs.
- Process `SandboxClaim`.
- Create, pause, resume, or delete Actors.
- Sync individual Actor state to Kubernetes CRDs.

### 6.3 Template Hash

ActorTemplate naming uses a content hash for stability:

```go
func computeSubstrateTemplateHash(spec *atev1alpha1.ActorTemplateSpec) (string, error) {
    data, err := json.Marshal(spec)
    if err != nil {
        return "", err
    }
    h := sha256.Sum256(data)
    return fmt.Sprintf("%x", h[:4]), nil
}
```

When `SandboxSet.spec.template` changes:
1. New hash → new ActorTemplate name.
2. New E2B creates use the new ActorTemplate.
3. Existing Actors are not migrated or rebuilt.
4. Old ActorTemplates are garbage collected when no Actors reference them (via OwnerReference to SandboxSet).

### 6.4 Scale Behavior

- **Scale up**: Controller updates `WorkerPool.spec.replicas`. Substrate adds worker pods. No Actors created.
- **Scale down**: Controller updates `WorkerPool.spec.replicas`. Substrate shrinks idle workers. Running Actors are not directly affected.

## 7. SubstrateInfra Design

### 7.1 Package Structure

```
pkg/sandbox-manager/infra/substrate/
  builder.go      # SubstrateInfraBuilder implementing infra.Builder
  infra.go        # SubstrateInfra implementing infra.Infrastructure
  sandbox.go      # SubstrateSandbox implementing infra.Sandbox
  client.go       # Substrate gRPC client wrapper
  template.go     # E2B template → ActorTemplate resolver
  metadata.go     # In-memory sandboxID → metadata store
  lock.go         # Per-actor keyed mutex
```

### 7.2 Key Data Structures

```go
type SubstrateInfra struct {
    client    *SubstrateClient
    metadata  SandboxMetadataStore
    locks     *KeyedLocker
    templates *TemplateResolver
    cache     cache.Provider
    proxy     *proxy.Server
}

type SandboxMetadata struct {
    SandboxID         string
    ActorID           string
    Namespace         string
    SandboxSetName    string
    ActorTemplateName string
    Owner             string
    Route             proxyutils.Route
    Phase             string
    Timeout           time.Time
    CreateTime        time.Time
    LastActiveTime    time.Time
    HibernateMode     string
}
```

### 7.3 Template Resolution

`TemplateResolver` maps an E2B `templateName` (SandboxSet name) to the corresponding `ActorTemplate` reference by reading the SandboxSet's status annotation (`substrate.agents.kruise.io/actor-template-name`).

### 7.4 SubstrateSandbox

`SubstrateSandbox` implements `infra.Sandbox` by wrapping `SandboxMetadata` and a Substrate gRPC client. Lifecycle operations delegate to the Control API:

| Method | Substrate RPC |
|---|---|
| `Pause()` | `PauseActor` (hibernate=pause) or `SuspendActor` (hibernate=suspend) |
| `Resume()` | `ResumeActor` |
| `Kill()` | `SuspendActor` (if running) + `DeleteActor` |
| `InplaceRefresh()` | `GetActor` (refresh state and route) |

Unsupported operations return errors: `TriggerReuse`, `CSIMount`, `CreateCheckpoint`, `Request`.

## 8. E2B API to Substrate Lifecycle Mapping

| E2B Operation | Substrate RPC | Notes |
|---|---|---|
| `create` | `CreateActor` + `ResumeActor` | Resolve ActorTemplate, create Actor (suspended), resume to running. |
| `connect` | `GetActor` / `ResumeActor` | If running, return route. If paused/suspended, resume first. |
| `pause` | `PauseActor` or `SuspendActor` | Based on `hibernateMode` annotation. |
| `kill` | `SuspendActor` + `DeleteActor` | Suspend if running, then delete. |
| `get/list` | Metadata Store + optional `GetActor` | Metadata store is primary; GetActor for verification if needed. |

## 9. Lifecycle Flows

### 9.1 Create

```
POST /sandboxes
  → sandbox-manager auth / quota check
  → Resolve templateName → SandboxSet → verify backend=substrate
  → Resolve ActorTemplateRef from SandboxSet status annotations
  → Allocate sandboxID / actorID
  → Acquire per-actor lock
  → CreateActor(actorID, actorTemplateNamespace, actorTemplateName)
  → ResumeActor(actorID, boot=false) — Actor starts running
  → Extract route from Actor.ateom_pod_ip
  → Write metadata store
  → Publish gateway route
  → Return E2B response
```

### 9.2 Connect / Wake

```
CONNECT /sandboxes/{sandboxID}
  → Read metadata store → get actorID
  → GetActor(actorID)
  → If STATUS_RUNNING: refresh timeout, return route
  → If STATUS_PAUSED / STATUS_SUSPENDED: ResumeActor(actorID, boot=false)
  → Wait until running / route ready
  → Update or publish route
  → Return connection info
```

### 9.3 Pause / Hibernate

```
POST /sandboxes/{sandboxID}/pause
  → Read metadata store → get actorID
  → Based on hibernateMode:
       pause   → PauseActor(actorID) — local snapshot, keeps worker
       suspend → SuspendActor(actorID) — external snapshot, frees worker
  → Remove or freeze route
  → Update metadata phase
  → Return success
```

### 9.4 Kill

```
DELETE /sandboxes/{sandboxID}
  → Remove route immediately (block new connections)
  → GetActor(actorID)
  → If running/resuming: SuspendActor(actorID)
  → DeleteActor(actorID)
  → Delete metadata
  → Delete per-actor lock
  → Return success
```

## 10. Idempotency & Concurrency

### 10.1 Per-Actor Lock

All lifecycle operations on the same Actor are serialized via `KeyedLocker`:

```go
type KeyedLocker struct {
    mu    sync.Mutex
    locks map[string]*sync.Mutex
}

func (l *KeyedLocker) Get(key string) *sync.Mutex {
    l.mu.Lock()
    defer l.mu.Unlock()
    m, ok := l.locks[key]
    if !ok {
        m = &sync.Mutex{}
        l.locks[key] = m
    }
    return m
}
```

### 10.2 Idempotency Rules

| Operation | Idempotency Rule |
|---|---|
| Create | Actor already exists → treat as success, read state. |
| Connect/Wake | Actor already running → return route directly. |
| Pause/Hibernate | Actor already paused/suspended → treat as success. |
| Kill | Actor not found → treat as success. |

## 11. Route & Metadata

### 11.1 Metadata Store

Since no `Sandbox` CRD exists, sandbox-manager maintains its own runtime metadata in-memory:

```
sandboxID → {
  actorID,
  sandboxSetName,
  actorTemplateName,
  owner,
  route (IP, ID, UID, Owner, State),
  phase,
  timeout,
  createTime,
  lastActiveTime,
  hibernateMode
}
```

### 11.2 Route Source

Routes come directly from Substrate Actor fields, not from `Sandbox.status.sandboxIp`:

```
Actor.ateom_pod_ip       → Route.IP
Actor.actor_id           → Route.UID
sandboxID                → Route.ID
owner                    → Route.Owner
actorStatusToPhase(...)  → Route.State
```

`SubstrateInfra` publishes routes on create/connect/resume success and removes routes before pause/kill.

## 12. Timeout & Cleanup

Managed by `sandbox-manager`, not by `SandboxSet`:

1. Periodic scan of metadata store.
2. For timed-out sandboxes, execute pause or kill based on policy.
3. Pause strategy: `PauseActor` / `SuspendActor`.
4. Kill strategy: `SuspendActor` + `DeleteActor`.
5. Clean up metadata and route entries.

`SandboxSet` does not participate in individual Actor timeout lifecycle.

## 13. Wiring & Configuration

### 13.1 sandbox-manager Flag

```
--substrate-addr string   Substrate control gRPC address (e.g. substrate-api:50051).
                          When set, uses Substrate as the sandbox backend.
```

### 13.2 Builder Integration

`SandboxManagerBuilder` gains a `WithSubstrateInfra(addr string)` method. The E2B controller's `Init()` branches on `substrateAddr`:

```go
if sc.substrateAddr != "" {
    builder = builder.WithSubstrateInfra(sc.substrateAddr)
} else {
    builder = builder.WithSandboxInfra()
}
```

### 13.3 Controller Registration

The `SandboxSetController` registers Substrate CRD schemes (`atev1alpha1.AddToScheme`) and checks `IsSubstrateBackend(sbs.Annotations)` at the top of `Reconcile` to branch to `reconcileSubstrate`.

## 14. Backward Compatibility

- No changes to existing CRD spec fields — fully backward compatible.
- `SandboxSet` without the `backend` annotation behaves exactly as before (creates Sandbox CRs).
- Existing sandboxes, claims, and E2B operations are unaffected.
- The substrate backend is opt-in via annotation.
- sandbox-manager defaults to the existing Sandbox CRD backend unless `--substrate-addr` is set.

## 15. Implementation Order

### Phase 1 — Substrate Dependency and Constants

- Add `github.com/agent-substrate/substrate` dependency.
- Define backend annotation constants in `pkg/controller/sandboxset/substrate.go`.
- Export `IsSubstrateBackend()`.

### Phase 2 — Core Infrastructure Components

- In-memory `SandboxMetadataStore` (`metadata.go`).
- Per-actor `KeyedLocker` (`lock.go`).
- Substrate gRPC client wrapper (`client.go`).
- E2B template → ActorTemplate resolver (`template.go`).

### Phase 3 — SubstrateSandbox and SubstrateInfra

- `SubstrateSandbox` implementing `infra.Sandbox` (`sandbox.go`).
- `SubstrateInfra` implementing `infra.Infrastructure` (`infra.go`).
- `SubstrateInfraBuilder` implementing `infra.Builder` (`builder.go`).

### Phase 4 — SandboxSet Controller Substrate Reconcile

- `reconcileSubstrate` in `sandboxset_controller.go`.
- `buildActorTemplateSpec` / `buildWorkerPoolSpec`.
- `ensureActorTemplate` / `ensureWorkerPool`.
- Substrate backend check at reconcile entry.

### Phase 5 — Wiring

- `WithSubstrateInfra()` on `SandboxManagerBuilder`.
- `--substrate-addr` flag in `cmd/sandbox-manager/main.go`.
- Conditional builder wiring in E2B controller `Init()`.

### Phase 6 — Reliability (Future)

- Timeout cleanup goroutine.
- Metrics and observability.
- WorkerPool / ActorTemplate readiness validation.
- Orphaned Actor reconciliation on restart.

## 16. Constraints & Limitations

This version explicitly does NOT support:

1. Managing Substrate Actor lifecycle via `Sandbox` CRD.
2. Claiming Substrate Actors via `SandboxClaim`.
3. Syncing individual Actor state to `Sandbox.status`.
4. Pre-created Actor pools (eager pooling).
5. Rolling upgrade of running Actors.
6. Decoupling `SandboxSet.spec.replicas` from worker pod count.
7. Clone, checkpoint, or CSI mount operations.
8. Metadata persistence across sandbox-manager restarts (in-memory only — orphaned Actors must be reconciled externally).

## 17. Open Questions

1. **Orphaned Actor cleanup on restart** — When sandbox-manager restarts, in-memory metadata is lost. How should orphaned Actors be detected and cleaned up? Options: (a) list Actors via gRPC on startup and reconcile, (b) external garbage collector, (c) persistent metadata store (Redis/etcd).

2. **Per-tenant concurrency limits** — Should SubstrateInfra enforce per-owner Actor limits, or delegate to Substrate's own quota system?

3. **Health monitoring** — Should sandbox-manager periodically `GetActor` to detect Actors that crashed or were externally deleted? What polling interval is acceptable?

4. **Hibernate mode override** — Should per-sandbox hibernate mode override (e.g., via E2B API parameter) be supported, or always use the SandboxSet-level annotation?

5. **ActorTemplate GC** — When should old ActorTemplates (from previous template revisions) be garbage collected? Via OwnerReference cascade when SandboxSet is deleted? Explicit controller logic?

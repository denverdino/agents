# Substrate Infrastructure

This package implements `pkg/sandbox-manager/infra` using Substrate gRPC as the sandbox backend.
Instead of managing Sandbox CRDs, it calls Substrate's Control API to manage Actor lifecycle.

## Responsibilities

- `SubstrateInfra` implements `infra.Infrastructure` by delegating to Substrate gRPC (`CreateActor`,
  `ResumeActor`, `PauseActor`, `SuspendActor`, `DeleteActor`).
- `SubstrateSandbox` wraps a Substrate Actor and implements `infra.Sandbox` for the manager layer.
- `InMemoryMetadataStore` maintains sandboxID -> actorID/owner/route/phase mapping in memory.
- `TemplateResolver` maps E2B template names (SandboxSet names) to Substrate ActorTemplate references.
- `KeyedLocker` serializes lifecycle operations on the same actor.
- `SubstrateClient` wraps the Substrate gRPC connection.

## Key Design Decisions

- Sandbox CRDs are NOT created or managed. Actor lifecycle is imperative via gRPC.
- Routes come from Substrate Actor pod IPs, not from Sandbox.status.sandboxIp.
- Metadata is stored in-memory. Loss on restart means orphaned actors must be reconciled.
- Clone and checkpoint operations are not supported in this version.
- The first container in SandboxSet.spec.template maps to ActorTemplate.pauseImage; additional
  containers map to ActorTemplate.containers.

## Local Guidance

- Never import `pkg/features` — this package runs in sandbox-manager, not the controller.
- Keep gRPC calls behind the `SubstrateClient` wrapper for testability.
- All Actor lifecycle operations on the same actor must go through `KeyedLocker`.
- Use `actorStatusToPhase()` for consistent Actor_Status -> phase string mapping.

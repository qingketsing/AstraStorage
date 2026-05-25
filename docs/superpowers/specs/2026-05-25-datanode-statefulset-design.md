# Datanode StatefulSet Design

## Goal

Replace the current Kubernetes `datanode` shape from `Deployment + emptyDir` to a single-replica `StatefulSet + PVC` so Pod restarts no longer discard local chunk data in the default PoC deployment.

## Scope

This change is intentionally minimal:

- keep a single `datanode` replica
- keep the existing gateway-facing `Service`
- keep the current `DATANODE_ADVERTISE_URL`
- add persistent storage through `volumeClaimTemplates`
- avoid introducing multi-replica datanode behavior, Pod-specific advertise URLs, or scheduling topology logic

## Design

The current `deploy/k8s/datanode` deployment uses `emptyDir`, which makes local chunk data ephemeral. The replacement will use:

- one headless `Service` referenced by the `StatefulSet.spec.serviceName`
- one normal `Service` that preserves the existing in-cluster access path used by `gateway`
- one single-replica `StatefulSet`
- one `volumeClaimTemplates` entry for `/data/datanode`

The StatefulSet will continue to expose port `10080`, use the same image and environment variables, and mount the PVC at `/data/datanode`.

## Risks

- Changing workload kind from `Deployment` to `StatefulSet` is not an in-place Kubernetes apply; live rollout requires deleting the old Deployment first.
- Existing clusters using the old manifest may lose currently stored ephemeral datanode data during migration, because that data already lives in `emptyDir`.
- This does not solve multi-datanode persistence or node-unique advertise URLs; it only makes the default single datanode restart-safe.

## Validation

- `kubectl kustomize deploy/k8s/datanode`
- optional live verification in `minikube`: delete old Deployment, apply new manifests, check StatefulSet rollout and PVC binding
- update PoC and Kubernetes docs so they no longer describe the default datanode shape as `Deployment + emptyDir`

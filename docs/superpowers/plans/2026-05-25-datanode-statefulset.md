# Datanode StatefulSet Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the default Kubernetes datanode deployment with a single-replica StatefulSet backed by a PVC.

**Architecture:** Keep the current single-datanode PoC topology, preserve the existing gateway-facing Service, and add a headless Service plus `volumeClaimTemplates` so the datanode Pod gets persistent local storage without introducing multi-node behavior.

**Tech Stack:** Kubernetes manifests, Kustomize, minikube validation.

---

### Task 1: Replace the workload kind

**Files:**
- Create: `deploy/k8s/datanode/headless-service.yaml`
- Create: `deploy/k8s/datanode/statefulset.yaml`
- Modify: `deploy/k8s/datanode/kustomization.yaml`
- Delete: `deploy/k8s/datanode/deployment.yaml`

- [ ] Write the StatefulSet manifest with `replicas: 1`, `serviceName`, existing env vars, probes, resources, and a `volumeClaimTemplates` entry mounted at `/data/datanode`.
- [ ] Add a headless Service used only by the StatefulSet network identity.
- [ ] Keep the existing normal Service unchanged for gateway traffic.
- [ ] Update `kustomization.yaml` to render the new resources and stop referencing the deleted Deployment manifest.

### Task 2: Update docs

**Files:**
- Modify: `README.md`
- Modify: `docs/poc.md`
- Modify: `docs/architecture/kubernetes-deployment.md`

- [ ] Replace references to `Deployment + emptyDir` as the default datanode shape with `single-replica StatefulSet + PVC`.
- [ ] Keep the documentation explicit that this is still a PoC-level single-node persistence improvement, not a multi-datanode HA design.

### Task 3: Validate manifests and live rollout

**Files:**
- Validate only

- [ ] Run `kubectl kustomize deploy/k8s/datanode` and confirm the manifest renders cleanly.
- [ ] If the local minikube cluster is available, delete the old Deployment and apply the new datanode manifests.
- [ ] Verify PVC binding and StatefulSet rollout status.
- [ ] Confirm the remaining AstraStorage Pods stay healthy after the datanode workload kind change.

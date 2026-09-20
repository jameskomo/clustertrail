#!/usr/bin/env bash
# Fills a cluster with everything the tour and the screenshots rely on:
# workloads in several states, metrics, a custom resource, a Helm release,
# and the Secret and ConfigMap the container detail reads.
#
# Safe to re-run. Never point this at a cluster you care about.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$HOME/.local/bin:$PATH"
export KUBECONFIG="${KUBECONFIG:-$ROOT/.kind-kubeconfig}"

echo ">> workloads"
kubectl apply -f "$ROOT/deploy/demo.yaml" >/dev/null

echo ">> metrics-server"
kubectl apply -f "$ROOT/deploy/metrics-server.yaml" >/dev/null

echo ">> config and credentials the container detail reads"
kubectl apply -f - >/dev/null <<'YAML'
apiVersion: v1
kind: Secret
metadata: { name: checkout-credentials, namespace: shop }
type: Opaque
stringData:
  DB_PASSWORD: not-a-real-password
  API_TOKEN: demo-value-not-a-real-token
---
apiVersion: v1
kind: ConfigMap
metadata: { name: checkout-settings, namespace: shop }
data:
  LOG_LEVEL: debug
  REGION: eu-west-1
YAML

echo ">> custom resource definition and two custom resources"
kubectl apply -f "$ROOT/deploy/crd-demo.yaml" >/dev/null 2>&1 || true
kubectl wait --for=condition=Established crd/backups.demo.clustertrail.dev --timeout=60s >/dev/null 2>&1
kubectl apply -f "$ROOT/deploy/crd-demo.yaml" >/dev/null 2>&1

echo ">> a Helm release, stored exactly the way Helm stores one"
python3 "$ROOT/scripts/helm-fixture.py" | kubectl apply -f - >/dev/null 2>&1

echo ">> waiting for the workloads to settle"
kubectl rollout status deploy/checkout -n shop --timeout=120s >/dev/null 2>&1 || true

cat <<'DONE'

Seeded. The cluster now has:
  - healthy workloads, one CrashLoopBackOff and one ImagePullBackOff, on purpose
  - CPU and memory, so the charts have something to draw
  - a Backup custom resource, to browse through its CRD
  - a shop-frontend Helm release with two revisions, to roll back
  - a Secret and ConfigMap wired into checkout's environment and mounts
DONE

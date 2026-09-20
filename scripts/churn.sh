#!/usr/bin/env bash
# Generates pod churn so the live table has something to show.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$HOME/.local/bin:$PATH" KUBECONFIG="${KUBECONFIG:-$ROOT/.kind-kubeconfig}"
while true; do
  for n in 8 2 5; do kubectl scale deploy/checkout -n shop --replicas=$n >/dev/null; echo "checkout → $n"; sleep 6; done
done

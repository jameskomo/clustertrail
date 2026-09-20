#!/usr/bin/env bash
# Runs the engine and the UI against the local kind cluster.
# Open http://127.0.0.1:5173/?token=dev when both are up. Ctrl-C stops both.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$HOME/.local/bin:$HOME/.local/go/bin:$PATH"
export KUBECONFIG="${KUBECONFIG:-$ROOT/.kind-kubeconfig}"   # never the GKE contexts by default

if ! kind get clusters 2>/dev/null | grep -qx clustertrail; then
  echo ">> creating kind cluster 'clustertrail'"; kind create cluster --name clustertrail --wait 120s
  "$ROOT/scripts/seed-demo.sh"
fi

echo ">> building engine"; (cd "$ROOT/engine" && go build -o bin/clustertrail ./cmd/clustertrail)
"$ROOT/engine/bin/clustertrail" serve --addr 127.0.0.1:7777 --token dev --data "$ROOT/.clustertrail-data" &
ENGINE=$!
trap 'kill $ENGINE 2>/dev/null || true' EXIT
(cd "$ROOT/app" && npx vite --host 127.0.0.1)

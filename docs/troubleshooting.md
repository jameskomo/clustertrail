# Troubleshooting

Every entry here is a problem that actually happened while building this.

## The UI loads but has no colours and uses a serif font

Vite cached an empty copy of the stylesheet. Restart the dev server with a
clean cache:

```sh
pkill -f "vite --host"; rm -rf app/node_modules/.vite; make dev
```

To confirm that is the cause, `curl -s http://127.0.0.1:5173/src/style.css`
and look for `const __vite__css = ""`.

## "No directory at /path/to/repo."

The path picked up a trailing character, usually a full stop from copying it
out of a sentence. ClusterTrail detects that case and says so. Paths are trimmed and
`~/` is expanded, so `~/code/infra` works.

## The pods table says "no metrics" and the CPU column is dashes

metrics-server is not installed in that cluster. For the local cluster:

```sh
kubectl --kubeconfig .kind-kubeconfig apply -f deploy/metrics-server.yaml
```

The copy in `deploy/` carries `--kubelet-insecure-tls`, which kind needs
because its kubelet serves a self-signed certificate. **Do not** apply that
flag to a real cluster.

## Charts say "collecting samples"

The engine polls metrics every 15 seconds and needs two readings to draw a
line, so about 30 seconds after you first open a chart. A pod that is not
running has no metrics at all, and the drawer says that instead of drawing an
empty chart.

## A cluster row is red, or namespaces never load

The engine could not authenticate to that context. ClusterTrail surfaces the API
server's error rather than hiding it. The usual causes are an expired token,
an `exec` plugin binary that is not on the engine's `PATH`, or a VPN that is
not connected. Test the same context with `kubectl get ns --context <name>`;
if that fails, ClusterTrail will too, for the same reason.

## Exec or port-forward fails on a pod that is running

Both need the pod to be `Running` with a started container. A pod in
`CrashLoopBackOff` has no process to attach to. For logs from a crashed
container, use the **previous** toggle in the Logs tab instead.

## Approving a change fails with a conflict

Someone else changed the object between your reading it and approving. That is
the protection working: ClusterTrail sends the resourceVersion you loaded, so it
will not silently overwrite. Close the drawer, reopen it, and redo the edit
against the current object.

## Drift reports everything as "running, not in Git"

The scan is comparing against a namespace your repository does not describe.
Either pick the namespace in the rail before scanning, or untick "also find
what is running but not in Git" to see only the objects your files cover.

## Helm shows no releases

ClusterTrail reads Helm's release Secrets, which carry `owner=helm`. Check they
exist:

```sh
kubectl get secret -A -l owner=helm
```

If the release is stored in ConfigMaps, ClusterTrail reads those too. If it is in a
custom backend, it will not be found.

## The engine will not start

- `refusing to listen on a non-loopback address` is deliberate. The engine
  binds loopback only; pass a `127.0.0.1` address.
- `address already in use` means an older engine is still running:
  `pkill -x clustertrail`.

## Explain says there is no model key

Add one in the Explain tab, or start the engine with `ANTHROPIC_API_KEY` set.
The key is held in memory for that engine process only, so restarting the
engine clears it.

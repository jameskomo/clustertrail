# Getting started

This takes you from a machine with nothing installed to ClusterTrail running against
a real cluster, then walks you through the parts worth seeing first.

## 1. What you need

| Tool | Why | Check |
|---|---|---|
| Docker | kind runs the test cluster inside a container. | `docker info` |
| Go 1.27+ | Builds the engine. | `go version` |
| Node 22+ | Builds and serves the UI. | `node --version` |
| kind | Creates the local Kubernetes cluster. | `kind version` |
| kubectl | Not required by ClusterTrail, but useful for checking its work. | `kubectl version` |

Rust is **not** needed yet. It becomes a requirement only when the Tauri shell
lands.

### Installing Go and kind without touching system directories

Both install cleanly under your home directory:

```sh
# Go
curl -sL "https://go.dev/dl/$(curl -s https://go.dev/VERSION?m=text | head -1).linux-amd64.tar.gz" -o /tmp/go.tgz
tar -C ~/.local -xzf /tmp/go.tgz          # gives you ~/.local/go

# kind
KV=$(curl -s https://api.github.com/repos/kubernetes-sigs/kind/releases/latest | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
curl -sL "https://github.com/kubernetes-sigs/kind/releases/download/$KV/kind-linux-amd64" -o ~/.local/bin/kind
chmod +x ~/.local/bin/kind

export PATH="$HOME/.local/bin:$HOME/.local/go/bin:$PATH"
```

`scripts/dev.sh` adds both of those directories to `PATH` itself, so you only
need the export for running `go` or `kind` by hand.

## 2. First run

```sh
make dev
```

That script does four things, and skips whatever is already done:

1. Creates a kind cluster named `clustertrail` if it does not exist.
2. Applies the demo workloads and metrics-server.
3. Builds `engine/bin/clustertrail`.
4. Starts the engine on `127.0.0.1:7777` and the UI on `127.0.0.1:5173`.

Open **http://127.0.0.1:5173/?token=dev**.

The token in that URL is what stops any other process on your machine from
using the engine to reach your clusters. In development it is the fixed string
`dev`; the engine generates a random one when you do not pass `--token`.

### Watching something happen

The demo cluster is deliberately static. In a second terminal:

```sh
make churn      # scales a deployment up and down every few seconds
```

## 3. Pointing it at a real cluster

The development setup uses an isolated kubeconfig at `.kind-kubeconfig`, so
the engine never sees your real contexts by accident. To browse your own
clusters, run the engine yourself:

```sh
KUBECONFIG=~/.kube/config ./engine/bin/clustertrail serve \
  --addr 127.0.0.1:7777 --token dev --data ~/.config/clustertrail
```

Every context in the file appears in the cluster picker. **Nothing connects
until you select it**, so listing contexts never triggers a cloud auth plugin
and never prompts for a login you did not ask for.

Authentication is whatever your kubeconfig says: client certificates, tokens,
OIDC, or the `exec` plugins that EKS, GKE and AKS use. ClusterTrail shells out to
the same plugin binaries `kubectl` does.

## 4. The tour

Seven things, in order, each about a minute.

**1. The overview tells you the state of the cluster in a sentence.**
It opens with something like "kind-clustertrail needs attention: 2 pods failing and
2 deployments degraded", then the live CPU and memory of every pod, the top
consumers, what needs attention, and the last hour of warning events.

**2. Click `payments` under "Needs attention".**
The drawer opens on its logs with the crash output. Tick **previous** to read
what the last container printed before it died. This is the "fewer clicks to
the log line" claim, and it is two.

**3. Open any `checkout` pod and read the container section.**
Image, state, ports, resource requests and limits, and both probes. Under it,
every environment variable with where its value comes from. `DB_PASSWORD`
reads "secret checkout-credentials.DB_PASSWORD"; press **Resolve** and ClusterTrail
fetches that Secret and decodes the key. Mounts show what backs them.

**4. Deployments → `checkout` → type 6 → Scale.**
Read the dialog. It says what will happen in a sentence, states the blast
radius, and shows the dry-run diff. Approve with Ctrl-Enter. Nothing was
written to the cluster until that keystroke.

**5. What changed.**
The screen that answers the first question of any incident. Make something
change first so there is material:

```sh
kubectl --kubeconfig .kind-kubeconfig set image deploy/catalog api=nginx:1.26-alpine -n shop
```

Within a few seconds the timeline shows the field write naming `kubectl-set`
as the actor, the rollout with the image that changed, and the events that
followed. Tick **only what a person did** to collapse it to human action.

**6. Drift → "Try it with the sample repo".**
One click scans `deploy/sample-repo` against the cluster. You get one object
that is in Git but not running, one that differs, and four that are running
with no file behind them. Open the one that differs to read its diff.

**7. Helm → `shop-frontend`.**
The values it was installed with, the manifest it rendered, its notes and its
revision history, read straight out of the cluster.

**8. Any pod → Explain.**
Add an Anthropic API key and ClusterTrail hands the object, its events and recent
logs to Claude, then shows the answer with a button to see exactly what was
sent. The model has no tools and cannot reach the cluster.

Then press **Ctrl-K** and type a few letters of anything.

## 5. Keyboard

| Key | Does |
|---|---|
| `Ctrl/⌘ K` | Command palette: screens, clusters, namespaces, objects. |
| `/` | Focus the filter box on a table. |
| `Esc` | Close the drawer, then clear the filter. In a dialog, reject. |
| `Ctrl/⌘ Enter` | Approve the change under review. |
| `Ctrl/⌘ S` | In the YAML editor, propose your edit. |

## 6. Resetting the demo

```sh
kubectl --kubeconfig .kind-kubeconfig delete svc shop-frontend -n shop   # restores the drift example
kind delete cluster --name clustertrail                                        # start over completely
```

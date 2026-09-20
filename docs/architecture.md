# Architecture

ClusterTrail is two processes: a Go engine that talks to Kubernetes, and a webview
that talks only to the engine. Everything else follows from that split.

```
┌─────────────────────── your machine ────────────────────────┐
│                                                             │
│  UI (Vue 3 + Vite)                                          │
│    screens, virtualised tables, drawer, editor, terminal    │
│         │                                                   │
│         │  one WebSocket, JSON frames, per-launch token     │
│         ▼                                                   │
│  clustertrail (Go)                                         │
│    kubeconfig + lazy sessions    change gate + audit chain  │
│    informers + row projections   drift  ·  helm  ·  explain │
│    logs · exec · port-forward    metrics cache + history    │
│         │                                                   │
└─────────┼───────────────────────────────────────────────────┘
          │  client-go: TLS, tokens, OIDC, exec plugins
          ▼
   your Kubernetes API servers
```

*(Pending: a Tauri shell around the UI. It adds window, tray, updater and
keychain, and supervises the engine. Nothing above changes when it lands.)*

## Why two processes

**client-go is the only complete Kubernetes client.** It handles every
authentication path a real kubeconfig can contain, including the `exec`
plugins EKS, GKE and AKS use, and it ships the informer machinery that makes
live tables cheap. That decides the engine is Go.

**The webview talks to the engine directly**, not through the desktop
framework. Routing table deltas through the shell would mean two
serialisations and an extra process hop per frame. A loopback WebSocket is one
hop, supports backpressure, and means the entire UI can be developed in a
plain browser with no desktop framework in the loop. It also makes the shell
swappable.

**The engine is a standalone binary.** That was not an accident: the same
functions behind the UI can back a CLI drift check in CI and an MCP server for
coding agents, neither of which costs anything extra to expose.

## Rows, not objects

The reason ClusterTrail is fast is that the engine never sends a whole object to a
table.

Each subscription carries a projection: a function from the typed object to a
row of the handful of fields a table shows, around 200 bytes. The engine
applies it on every informer event, batches upserts and deletes, and emits at
most every 100 ms. Ten thousand pods is about 2 MB on first sync and then
deltas. The full object is fetched only when a drawer opens.

Rows are keyed by `namespace/name`, and upserts and deletes are idempotent, so
the client can apply a delta in any order relative to its snapshot without
reconciling.

## Lazy sessions

The kubeconfig is read at startup, but **no context is connected until a
screen asks for one**. Listing clusters therefore never triggers a cloud auth
plugin, never prompts for a login, and never blocks on a VPN that is not up.
Each session lazily creates informer factories per namespace, a metrics poller
that starts with its first subscriber, and its own stop channel.

## One way to write

Every mutation in the codebase goes through `internal/change`. Propose runs a
server-side dry run and records a pending Change with a real diff and a
computed blast radius; decide records the decision; apply does the work and
records the outcome. Each step appends to a hash-chained audit log.

That is what makes the agent story structural rather than a promise: an author
of type `agent` cannot advance its own Change, and the enforcement is in the
engine, not in the interface.

## The kind registry

One table drives everything: the GVR to watch, whether it is namespaced, the
projection, and whether the kind can be scaled or restarted. Tables, the
drawer, the palette, the create dialog and the change gate all read from it.
Adding a kind is an entry plus a projection plus a column spec.

Custom resources bypass the table entirely: the engine fetches the CRD and
builds a Kind at request time from the printer columns the CRD declares, so a
custom resource's table matches `kubectl get` without anyone writing code.

## Local state

SQLite was the original plan. Today the engine keeps the audit log as a
hash-chained JSONL file, snapshots as one small JSON file per subscription,
and metrics history in memory; the UI keeps preferences in browser storage.
Each of those has exactly one writer and no queries, so a database would add
ceremony without answering a question anyone asks. It earns its place when
saved views and cross-session search arrive.

## What runs where

| Concern | Lives in | Why |
|---|---|---|
| Kubernetes credentials | Engine | Never reaches the browser or the network. |
| Informers and watches | Engine | One connection per cluster, shared by every screen. |
| Row projection | Engine | Keeps the wire and the DOM small. |
| Sorting and filtering | UI | Instant, no round trip. |
| Diffing | Engine, via the API server | A dry run is the only trustworthy diff. |
| Port forwards | Engine | Survive a UI reload; die with the engine. |
| Model key | Engine memory | Never on disk, never in the audit log. |
| Theme, pane sizes, last repo path | Browser storage | Per-viewer conveniences, nothing more. |

## Performance budgets

These are the numbers the design is built around, and the ones worth measuring
against Freelens and Aptakube before any public claim:

| Measure | Target | Where it stands |
|---|---|---|
| Launch to live rows, warm cluster | under 2.5 s | met on the demo cluster; first pod snapshot arrives about 110 ms after connect |
| Cold launch to first row, from cache | under 1.0 s | met; 8 ms from the cache against 109 ms live, measured on a cold engine |
| Click a pod to first log line | under 500 ms | met |
| 10 000 rows, scrolling | 60 fps | virtualised, not yet measured at that size |
| Resident memory, three clusters | under 200 MB | not measured |
| Installer size | under 40 MB | pending the shell; the engine binary alone is about 50 MB and will need trimming |

The mechanisms that buy them: row projections, 100 ms delta batching, a
virtualised table, lazy cluster connect, and a native engine binary.

See [development](development.md) for the repository layout and how to add a
kind, and [roadmap](roadmap.md) for what is left.

## How the build actually went

The original plan put the desktop shell in week one and the change gate in
week two. The order that worked was the reverse of the first and identical for
the second.

| Step | What landed |
|---|---|
| 1 | Engine serving pod rows over the socket; Vue table rendering them live in a plain browser. |
| 2 | The kind registry, so every other resource was configuration rather than code. |
| 3 | **The change gate and audit log, with the first mutation.** Scale, restart, delete and YAML edit all went through it from the start. |
| 4 | Drawer: events, logs, YAML editor, terminal, container and environment detail. |
| 5 | Metrics with in-memory history, and the charts that read it. |
| 6 | Drift, Helm and explain-this. |
| 7 | Pending: the Tauri shell. |

Two things are worth carrying forward. **Building the gate with the first
write rather than later** meant no mutation path ever existed outside it, so
the guarantee is structural instead of retrofitted. **Leaving the shell until
last** cost nothing, because the UI never depended on it.

## Decisions taken

| Decision | Choice |
|---|---|
| Engine language | Go, for client-go. Nothing else handles every kubeconfig auth path. |
| Shell | Tauri 2, kept thin. Tauri 3 is alpha and Wails 3 is beta; neither offers anything this needs. |
| UI to engine transport | One loopback WebSocket with a per-launch token, JSON frames. |
| Where the gate lives | The engine, so no front end can skip it. |
| Diffing | Server-side dry run, never text comparison. |
| Helm | Read Helm's own release objects rather than depend on the Helm SDK. |
| Phone, when it comes | A web app with Web Push from the relay, not a native app. |
| Licence | Apache 2.0 for the client. |

Open, and yours to decide: the name, the first cloud to verify against,
design partners, and the spending cap and stop date.

## Local environment

Go 1.27 at `~/.local/go`, kind 0.33 at `~/.local/bin`, and a kind cluster
named `clustertrail` on Kubernetes 1.37, isolated in `.kind-kubeconfig` so the
engine never sees your real contexts by accident. The Rust toolchain is the
only prerequisite still missing, and only the Tauri shell needs it. See
[getting started](getting-started.md).

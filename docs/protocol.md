# The local protocol

The UI and the engine speak over **one WebSocket** at
`ws://127.0.0.1:<port>/ws?token=<token>`. Frames are JSON objects with a
`type` field. Requests and subscriptions are multiplexed by `id`.

There is also `GET /healthz`, which returns `ok` and takes no token, so a
supervisor can check the engine without holding a credential.

## Conventions

- **The client picks the `id`.** Answers carry it back. Subscriptions keep
  using theirs until unsubscribed.
- **Empty arrays are omitted**, not sent as `[]`. Treat a missing array as
  empty. This caught out the first test client written against it.
- **Rows are keyed by `namespace/name`** (just `name` for cluster-scoped
  kinds). Upserts and deletes are idempotent, so the UI can apply them in any
  order relative to the snapshot.
- **A slow client is disconnected**, not buffered without limit. It reconnects
  and gets a fresh snapshot.

## Client messages

| `type` | Fields | Does |
|---|---|---|
| `clusters` | none | List kubeconfig contexts. Connects to nothing. |
| `subscribe` | `id`, `cluster`, `kind`, `namespace` | Stream rows of a kind. `kind` is a registry key (`pods`), an API kind (`Pod`), or `crd:<crd name>` for a custom resource. |
| `subscribe` | `id`, `cluster`, `kind: "logs"`, `namespace`, `name`, `container`, `previous`, `tail` | Stream a container log. |
| `unsubscribe` | `id` | Stop a subscription. |
| `events` | `id`, `cluster`, `kind`, `namespace`, `name` | Events for one object. `kind` here is the API kind. |
| `get` | `id`, `cluster`, `kind`, `namespace`, `name` | One object as YAML, without `managedFields`. |
| `history` | `id`, `cluster`, `name` | Metrics samples. `name` is `pod/<ns>/<name>`, `node/<name>` or `cluster`. |
| `change.propose` | `id`, `cluster`, `intent`, `plan[]` | Dry-run a plan and create a pending Change. Writes nothing. |
| `change.decide` | `id`, `name` (the change id), `decision`, `note` | Approve or reject. Approving applies. |
| `change.list` | `id` | Every change plus the last 200 audit entries. |
| `timeline` | `id`, `cluster`, `namespace`, `minutes` | Merge rollouts, field writes, events and audit entries for a window. |
| `drift.scan` | `id`, `cluster`, `path`, `namespace`, `includeUnmanaged` | Compare a directory of YAML with the cluster. |
| `drift.capture` | `id`, `path`, `dir`, `branch`, `push`, `objects[]` | Write objects into a repository on a new branch. Touches no cluster. |
| `helm.list` | `id`, `cluster`, `namespace` | Newest revision of every release. |
| `helm.get` | `id`, `cluster`, `namespace`, `name`, `revision` | One release with values, manifest, notes and history. `revision: 0` means latest. |
| `helm.rollback.plan` | `id`, `cluster`, `namespace`, `name`, `revision` | The objects an older revision would apply. Writes nothing. |
| `explain.preview` | `id`, `cluster`, `kind`, `namespace`, `name` | Gather the evidence and return it. Sends nothing anywhere. |
| `explain` | `id`, `cluster`, `kind`, `namespace`, `name`, `evidence` | Ask the model, using the evidence the person reviewed. Fails if no evidence is supplied, so nothing can be sent that was not seen. |
| `explain.status` | `id` | Whether a model key is available. |
| `explain.key` | `id`, `key` | Set the model key for this engine process. Empty string clears it. |
| `exec.start` | `id`, `cluster`, `namespace`, `name`, `container`, `cols`, `rows` | Open a shell. |
| `exec.input` | `id`, `data` | Base64 bytes to the shell. |
| `exec.resize` | `id`, `cols`, `rows` | Resize the TTY. |
| `exec.stop` | `id` | Close the shell. |
| `forward.start` | `id`, `cluster`, `namespace`, `name`, `port`, `localPort` | Forward a pod port. `localPort: 0` picks a free one. |
| `forward.stop` | `id`, `name` (the forward id) | Stop one. |
| `forward.list` | `id` | List active forwards. |

### The plan

`change.propose` takes a list of operations:

```json
{ "op": "scale",   "kind": "deployments", "namespace": "shop", "name": "checkout", "replicas": 5 }
{ "op": "restart", "kind": "deployments", "namespace": "shop", "name": "checkout" }
{ "op": "delete",  "kind": "pods",        "namespace": "shop", "name": "checkout-abc" }
{ "op": "apply",   "kind": "services",    "namespace": "shop", "name": "frontend", "yaml": "apiVersion: v1\n..." }
```

`scale` requires a scalable kind, `restart` a kind that supports rollout
restart; the engine rejects the others rather than doing something surprising.

## Server messages

| `type` | Carries | When |
|---|---|---|
| `hello` | `version` | On connect. |
| `clusters` | `clusters[]` | Answer to `clusters`. |
| `snapshot` | `id`, `rows[]`, `metricsAvailable`, `cached`, `cachedAt` | Once from the on-disk cache if there is one, immediately; then once from the informer when it syncs. A cached snapshot sets `cached: true` and carries the time it was saved. |
| `delta` | `id`, `upsert[]`, `delete[]` | At most every 100 ms while anything changed. |
| `log` | `id`, `lines[]`, `eof` | Batched log lines. |
| `events` | `id`, `events[]` | Answer to `events`. |
| `object` | `id`, `object` | YAML, answer to `get`. |
| `history` | `id`, `samples[]` | Metrics samples, oldest first. |
| `change` | `id`, `change` | Answer to `change.propose` and `change.decide`. |
| `changes` | `id`, `changes[]`, `audit[]` | Answer to `change.list`. |
| `timeline` | `id`, `timeline` | Answer to `timeline`. |
| `drift` | `id`, `drift` | Answer to `drift.scan`. |
| `capture` | `id`, `capture` | Answer to `drift.capture`, with the branch, the files and a compare URL. |
| `releases` | `id`, `releases[]` | Answer to `helm.list`. |
| `release` | `id`, `release` | Answer to `helm.get`. |
| `rollback` | `id`, `rollback` | Answer to `helm.rollback.plan`. |
| `explain.preview` | `id`, `evidence` | What would be sent, for review. |
| `explain` | `id`, `answer`, `evidence` | Answer to `explain`. |
| `explain.key` | `id`, `hasKey`, `keyFromEnv` | Answer to `explain.status` and `explain.key`. `keyFromEnv` means the only key available came from the environment rather than from this session. |
| `term` | `id`, `data` or `eof` + `message` | Terminal bytes, base64. |
| `forwards` | `id`, `forwards[]` | Answer to the forward messages. |
| `error` | `id`, `message` | Anything that failed. |

## A row

```json
{
  "key": "shop/checkout-5d986c6d9c-6jdw8",
  "name": "checkout-5d986c6d9c-6jdw8",
  "namespace": "shop",
  "createdAt": "2026-09-18T13:54:16Z",
  "status": "Running",
  "health": "ok",
  "cells": { "ready": "1/1", "restarts": 0, "node": "clustertrail-control-plane",
             "cpu": 12, "mem": 7340032, "containers": ["web"] }
}
```

`health` is `ok`, `warn`, `bad` or absent, and drives the status dot. `cells`
holds whatever that kind's projection produced; the UI's column spec decides
which ones to show and how to format them.

## Worked example

```js
const ws = new WebSocket('ws://127.0.0.1:7777/ws?token=dev')
ws.onopen = () => {
  ws.send(JSON.stringify({ type: 'clusters' }))
  ws.send(JSON.stringify({ type: 'subscribe', id: 's1',
    cluster: 'kind-clustertrail', kind: 'pods', namespace: 'shop' }))
}
ws.onmessage = (e) => {
  const m = JSON.parse(e.data)
  if (m.type === 'snapshot') console.log(m.rows.length, 'pods')
  if (m.type === 'delta') console.log('changed:', (m.upsert ?? []).map(r => r.name))
}
```

Measured against the demo cluster: `clusters` answers in about 10 ms, the
first pod snapshot in about 110 ms including the informer's initial list.

## Why one socket

Every screen is a subscription or a request, and both need ordering with
respect to each other. One connection gives that for free, gives the engine
one place to apply backpressure, and means the UI can run in a plain browser
during development with no desktop framework in the loop.

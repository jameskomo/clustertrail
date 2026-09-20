# Development

## Layout

```
engine/                       Go module, the cluster engine
  cmd/clustertrail/          serve
  internal/
    cluster/                  kubeconfig, lazy sessions, metrics cache + history
    kinds/                    the registry: GVR, namespaced, projection, per-kind abilities
    rows/                     one projection per kind (object -> table row)
    watch/                    informer subscriptions, logs, events, object fetch
    change/                   the gate: propose, dry-run diff, blast radius, decide, apply
    audit/                    append-only hash-chained log
    drift/                    render YAML dir, compare, classify
    helm/                     read Helm's release objects out of the cluster
    explain/                  evidence gathering and the model call
    term/                     exec sessions
    forward/                  port-forward manager
    api/                      WebSocket server, frame types
app/                          Vue 3 + Vite, the UI
  src/api/                    types mirroring the Go frames, socket client
  src/kinds.ts                per-kind column specs and nav grouping
  src/state.ts                selection and screen state
  src/composables/            subscriptions, logs, history, changes, forwards, theme, format
  src/components/             table, drawer, editor, terminal, charts, dialogs, palette
  src/screens/                overview, resource, changes, drift, helm
deploy/                       demo workloads, metrics-server, demo CRD, sample drift repo
scripts/                      dev.sh, churn.sh
docs/                         this documentation
```

23 Go files and 40 TypeScript and Vue files at the time of writing.

## The build loop

```sh
make dev        # cluster if needed, build engine, run engine + UI
make engine     # build the engine only
make app        # production build of the UI
make test       # go vet + go test
make churn      # generate pod churn to watch the tables move
```

The UI hot-reloads. The engine does not, so rebuild and restart it after Go
changes:

```sh
pkill -x clustertrail
KUBECONFIG=$PWD/.kind-kubeconfig ./engine/bin/clustertrail serve \
  --addr 127.0.0.1:7777 --token dev --data $PWD/.clustertrail-data &
```

## Adding a resource kind

Four edits, no new machinery:

**1. Project it.** In `engine/internal/rows/`, write a function from the typed
object to a `Row`. Put the columns a table needs in `Cells`; set `Status` and
`Health` if the kind has a meaningful state.

```go
func ProjectWidget(w *v1.Widget) Row {
	r := base(w.ObjectMeta)
	r.Status, r.Health = "Ready", HealthOK
	r.Cells["size"] = w.Spec.Size
	return r
}
```

**2. Register it.** In `engine/internal/kinds/kinds.go`:

```go
register(Kind{Key: "widgets", Kind: "Widget",
	GVR:        schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"},
	Namespaced: true, Project: typed(rows.ProjectWidget)})
```

Set `Scalable` or `Restarts` if the change gate should allow those operations.

**3. Give it columns.** In `app/src/kinds.ts`, add a `KindSpec` with its
group, and columns referencing the cell keys you set.

**4. Nothing else.** The table, drawer, filtering, sorting, counts, palette
entries and the create dialog all work from the registry and the spec.

Custom resources skip all of this: the engine builds a Kind from the CRD's own
printer columns at request time.

## Testing

```sh
make test         # go vet and the unit suite
make integration  # the tests that need a live cluster
```

The suite has two halves. Unit tests pin the pure logic; integration tests,
behind the `integration` build tag, prove the things only a real API server
can: that proposing writes nothing, that rejecting leaves the cluster alone,
that the diff comes from the server rather than from text comparison, that
drift classifies missing, modified and unmanaged correctly, and that the
timeline reads the actor out of `managedFields`. Each one creates a throwaway
namespace and removes it, so a failed run leaves nothing behind. CI runs both,
creating a kind cluster for the second.

32 tests cover the parts where a mistake would be expensive and the logic is
pure enough to pin down:

| Package | What is tested |
|---|---|
| `audit` | That the chain links, that it verifies, and that it detects both a tampered entry and a removed one, naming the sequence number where the chain breaks. |
| `change` | That the blast radius is derived from the plan rather than claimed; that a scale needs both a live object and a replica count; that a restart stamps the pod template; that an apply carries the resourceVersion it read, which is what turns a concurrent edit into a conflict instead of a silent overwrite. |
| `drift` | That the renderer reads every YAML file and skips values files, vendor directories and unknown kinds; that multi-document files split correctly and a line of dashes inside a string does not split one; that cluster-managed objects are not reported as drift. |
| `timeline` | That `managedFields` trees become readable paths, stop at a sensible depth, and ignore metadata and status; that image deltas describe only real changes; that causes are kept and consequence events dropped. |

Coverage is 89% in `audit` and lower elsewhere, because the remaining paths in
those packages need a live API server. Those are covered by the two harnesses
below, which should become a checked-in integration suite running against kind
in CI.

What exists but is not yet checked in:

- **Protocol scripts.** Small Node programs that open the socket, exercise a
  feature, and print what came back. They caught the omitted-empty-array
  convention and a float leaking into a chart label.
- **A browser driver.** A script that drives headless Chrome over the DevTools
  protocol, clicks through the screens, screenshots each one and asserts on
  the rendered text. It caught a stylesheet that Vite had cached empty, a
  broken `v-if` chain, and a button that vanished behind an error state.

Both live in the scratch directory rather than the repository, and turning
them into a checked-in suite is the obvious next piece of work. Start with the
change gate and the drift classifier: both are pure enough to test directly
against a kind cluster in CI.

## Conventions

- **Types flow one way**, Go to TypeScript. `app/src/api/types.ts` mirrors
  `engine/internal/api/frames.go` by hand today; generating it is a small job
  worth doing before the surface grows further.
- **Comments say why, not what.** The interesting comments in this codebase
  explain why rows are projections, why the diff is a dry run and why the gate
  lives in the engine.
- **Empty arrays are omitted on the wire.** Clients must tolerate it.
- **No secret goes to disk.** The model key lives in memory; the audit log
  records that a shell was opened, never what was typed into it.

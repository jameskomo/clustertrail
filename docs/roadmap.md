# Status and roadmap

## Built and working

Everything in [features](features.md), exercised against a local Kubernetes
1.37 cluster: twenty-one resource kinds plus custom resources, the overview
with live charts, the **what changed** timeline, an on-disk snapshot cache that paints a table
in 8 ms on a cold start, the detail drawer with
container and environment inspection, logs, the YAML editor, terminals, port
forwards, the change gate with its dry-run diffs and hash-chained audit log,
drift, Helm browsing, explain-this, creation from templates, the command
palette, and light and dark themes.

Beyond the desktop client, the same engine ships two more surfaces:

- **[`clustertrail drift`](cli.md)**, a CI check that exits non-zero on
  drift, so the editor and the pipeline use identical logic.
- **[`clustertrail mcp`](mcp.md)**, a read-only MCP server so coding agents
  can understand a cluster without being able to change it.

**32 tests** cover the audit chain, the change gate's derivations, the drift
renderer and the timeline's parsing.

## The public site

`site/` holds a single-page site built from the same design tokens as the
product, with screenshots taken from a live cluster rather than mocked. It is
static, so it can sit on Cloudflare Pages or behind an existing tunnel; both
are documented in [site/README.md](../site/README.md).

## Pending: the Tauri desktop shell

**This is the one unbuilt piece of the client.** ClusterTrail runs in a browser
today.

It was left until last on purpose. The UI talks to the engine over a local
WebSocket rather than through the desktop framework, so the shell is a
container, not a dependency: window, tray, auto-update, keychain for the model
key, and supervising the engine process. Nothing in the UI or the engine has
to change for it to land.

What it needs:

- The Rust toolchain, the only new prerequisite.
- A thin `src-tauri` that spawns the engine with a random per-launch token,
  reads the address it prints, and passes both to the webview.
- Signed builds in CI: an Apple Developer account for macOS, Azure Trusted
  Signing for Windows.

Until then, `make dev` and a browser is the whole product, and every feature
above works there.

## Smaller gaps in what is built

Honest limits, each noted in its feature section too:

| Area | Limit |
|---|---|

| Drift | Does not resolve remote Kustomize bases or fetch chart dependencies, because a drift check should not reach the network. |
| Helm | Rollback applies an old revision's objects through the review gate but does not rewrite Helm's release history, which the dialog states. |

| Types | `app/src/api/types.ts` mirrors the Go frames by hand; it should be generated. |
| Tests | Unit and integration suites exist and run in CI. The interface is still checked by driving a browser by hand rather than by a checked-in harness. |

## Deliberately deferred

From the original proposal, these belong to a later tier and stay unbuilt
until design partners commit to paying for them. They are not missing work:

- The cloud relay, the only server-side component.
- Phone approvals, planned as a web app with Web Push served by the relay, so
  there is no second UI codebase and no app store.
- The proposals engine: an agent that reads events, logs and rollout history
  during an incident and writes a proposal into the same review queue a human
  change goes through.
- Team sync and a shared audit log, verifiable against members' local chains.

The architecture is already shaped for them. Proposals add an author type to a
record that already exists, and the relay replicates audit entries it does not
originate. Neither changes the data model.

# Contributing

Thanks for considering a contribution.

## Before you start

- **Security issues:** do not open a public issue. See [SECURITY.md](SECURITY.md).
- **Large changes:** open an issue first. A design discussion before you write
  code saves us both a rejected pull request.
- **Small fixes:** just send the pull request.

## Developer Certificate of Origin

All commits must be signed off, certifying you have the right to submit the
code under the project's licence:

```sh
git commit -s -m "your message"
```

## Getting set up

You need Docker, Go 1.27, Node 22 and kind. Everything else is in
[docs/getting-started.md](docs/getting-started.md).

```sh
make dev     # kind cluster if missing, engine, and a hot-reloading interface
make test    # go vet and the Go suite
make bundle  # the single binary a user downloads
```

`scripts/seed-demo.sh` fills a cluster with workloads in every state, metrics,
a custom resource and a Helm release, so you can see every screen do something.

## Where things live

[docs/development.md](docs/development.md) has the layout and a worked example
of adding a resource kind. The short version: the engine is Go and owns
everything that touches a cluster; the interface is Vue and owns nothing but
presentation; they speak one WebSocket described in
[docs/protocol.md](docs/protocol.md).

## The rules that are not style preferences

These hold the product's promises up. A change that breaks one needs a very
good argument:

- **Every cluster write goes through `internal/change`.** There is exactly one
  path that mutates a cluster, and its diffs come from a server-side dry run.
  Do not add a second one.
- **The model gets no tools that can write.** The explain feature and the MCP
  server are read-only by construction, not by convention.
- **The engine binds loopback and requires its token.** Both are checked at
  startup and on every connection.
- **Rows are projections.** Tables receive small row structs, never whole
  objects. This is why ten thousand pods cost what forty do.
- **Empty arrays are omitted on the wire.** Clients tolerate it.

## What good looks like here

- **Comments say why, not what.** The interesting ones in this codebase explain
  why the diff is a dry run and why the gate lives in the engine.
- **Tests cover the expensive mistakes.** The audit chain, the gate's
  derivations and the drift renderer are all pure enough to test directly. New
  logic in those areas should arrive with tests.
- **User-facing text is plain.** Sentence case, active voice, and an error says
  what happened and what to do about it. "No directory at /path. /path-without-
  the-dot does exist" beats a raw `stat` error.
- **Screens are checked, not assumed.** If you change the interface, run it and
  look at it.

## Licence

By contributing you agree your work is licensed under
[Apache 2.0](LICENSE), the project's licence.

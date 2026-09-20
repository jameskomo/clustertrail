# The command line

The engine is one binary with three entry points. The desktop client runs
`serve`; the other two exist because the engine already knows how to do the
work, and exposing it costs almost nothing.

```
clustertrail serve   the local engine the desktop client talks to
clustertrail drift   compare a repository with a cluster; exits 1 on drift
clustertrail mcp     read-only MCP server over stdio, for coding agents
```

This page covers `drift`. For `mcp`, see [the MCP server](mcp.md).

## Why this matters

No desktop Kubernetes client runs in a pipeline. Putting the same drift engine
behind a command means the check that catches hand-edits in the editor also
catches them in CI, using identical logic, so the two can never disagree.

## Usage

```
clustertrail drift --repo DIR [--cluster CONTEXT] [--namespace NS]
                    [--unmanaged] [--json] [--quiet] [--kubeconfig PATH]
```

| Flag | Meaning |
|---|---|
| `--repo` | Directory of Kubernetes YAML to compare. Defaults to the working directory. |
| `--cluster` | Kubeconfig context. Defaults to the current context. |
| `--namespace` | Restrict the comparison to one namespace. |
| `--unmanaged` | Also report objects running with no file behind them. |
| `--json` | Print the full report, including every diff, as JSON. |
| `--quiet` | Print nothing. Use the exit code. |

### Exit codes

| Code | Meaning |
|---|---|
| 0 | The cluster matches the repository. |
| 1 | Something has drifted. |
| 2 | The command could not run: bad path, unreachable cluster, bad flag. |

The separation matters in CI. A failed comparison and an unreachable cluster
are different problems, and a pipeline that treats them the same will
eventually pass while blind.

## Output

```
$ clustertrail drift --repo deploy/sample-repo --cluster kind-clustertrail --namespace shop
kind-clustertrail vs deploy/sample-repo: 2 files

In the repository, not in the cluster (1)
  shop Service/shop-frontend  (frontend-service.yaml)

Different in the cluster (1)
  shop Deployment/checkout  (checkout.yaml)

1 in sync. Run with --json for the diffs.
```

With `--json` you get the whole report, each item carrying its `state`,
`kind`, `namespace`, `name`, `file`, and a unified `diff` for anything
modified. That is the form to pipe into a pull request comment.

## In a pipeline

```yaml
# .github/workflows/drift.yml
name: drift
on:
  schedule: [{ cron: "0 * * * *" }]
  workflow_dispatch:

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Configure cluster access
        run: |
          mkdir -p ~/.kube
          echo "${{ secrets.KUBECONFIG }}" > ~/.kube/config
      - name: Check for drift
        run: clustertrail drift --repo deploy --unmanaged
```

The job fails when the cluster and the repository disagree. Add `--json` and
post the output as a comment if you want the diff in the pull request.

### What it will and will not catch

It catches an object edited in place, an object deleted from the cluster, an
object added to the cluster by hand, and a file added to the repository that
was never applied.

It does not catch drift in anything it cannot render. Today that means plain
YAML only: Kustomize overlays and Helm charts are not rendered yet, so a
repository built from them will report its templates as unreadable rather than
comparing them.

## Credentials

`drift` uses your kubeconfig exactly as `kubectl` does, including `exec`
plugins. In CI that usually means a service account token with read access.
The command never writes to a cluster, so a read-only credential is enough,
and is what you should give it.

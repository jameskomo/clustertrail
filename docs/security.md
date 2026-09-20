# Security model

The product's premise is that an engineer can show ClusterTrail to a security
reviewer and have the answer be yes. This is what that reviewer needs to know.

## What ClusterTrail is allowed to do

**Exactly what your kubeconfig allows, and nothing else.** ClusterTrail installs no
controller, no operator, no agent and no CRD in your clusters. It authenticates
as you, using the credentials already in your kubeconfig, including the `exec`
plugins that EKS, GKE and AKS use. RBAC is inherited, so the review is the
same review you already did for `kubectl`. If your token cannot read Secrets
in a namespace, neither can ClusterTrail.

## The local attack surface

Requests must arrive addressed to the listener. Binding to 127.0.0.1 stops a
packet from another machine, but it does not stop a browser: a page on a
domain whose DNS answer is 127.0.0.1 reaches this process, and the browser
then treats it as same-origin, which makes an origin allowlist useless. The
Host header is checked against the address the engine bound, which is what
actually ties a request to how the engine was reached.

The token is still what stops that page from doing anything, and it remains
in the page URL, so it is in your browser history. On a machine you share
with another account, the token also reaches that account through the
browser's command line when `--open` launches it. If that is your situation,
start without `--open` and paste the URL yourself.

The engine is a local HTTP server, which is a real surface, so:

- **It binds loopback only.** `main` refuses to start if the resolved listen
  address is not a loopback address.
- **Every connection needs a per-launch bearer token**, compared in constant
  time, passed either in the `Authorization` header or the query string. The
  desktop shell generates a random token per launch; `--token` is for
  development.
- **Origins are restricted** to localhost and the Tauri origins.
- Without the token, `/ws` returns 401. Only `/healthz` is open, and it
  returns the string `ok`.

This matters because a process that could reach the engine could reach your
clusters with your credentials. The token is what prevents that.

## What is stored, and where

| Data | Where | Notes |
|---|---|---|
| Audit log | `<data dir>/audit.jsonl` | Append-only, hash-chained, mode 0600. |
| Model API key | Engine memory only | Never written to disk, never logged, never in the audit trail. |
| Kubeconfig | Not stored | Read from disk each launch; ClusterTrail never copies or forwards it. |
| Metrics history | Engine memory only | 30 minutes, lost on restart. |
| UI preferences | Browser local storage | Theme, last repository path, pane size. |

The data directory defaults to your OS config directory, `~/.config/clustertrail` on
Linux. Set it with `--data`.

## The change gate

Every mutation goes through one path in `internal/change`. There is no second
way to write to a cluster in the codebase.

1. **Propose.** The plan is dry-run against the API server. Nothing is
   written. A diff and a blast radius are computed and the Change is recorded
   as `pending`.
2. **Decide.** A human approves or rejects. Approval is recorded before the
   work starts.
3. **Apply.** The operations run. Success or failure is recorded.

Each of those appends to the audit log.

### Blast radius is computed, not claimed

The namespaces, kinds, object count, delete count and whether Secrets are
involved are derived by the engine from the plan itself. When the author is an
agent, the model cannot understate what it is asking for, because it does not
write that field.

## Why the agent cannot act

This is the part worth reading closely.

The explain feature builds a text prompt from the object, its events and its
logs, sends it, and displays the reply. **The model is given no tools.** There
is no tool-use loop, no function calling, and no path from a model response
back into the engine's Kubernetes clients. A reply is a string that is
rendered.

When the proposals engine arrives, the same property is meant to hold
structurally: the agent's only write is `propose_change`, which creates a
`pending` record, and only a human decision can advance it. The rule lives in
the engine, not in the interface, so a different or malicious front end cannot
skip it.

### What is sent to the model

You see it before it is sent, not after. **Prepare a question** collects the
evidence locally and shows it to you; nothing leaves the machine until you
press **Send it**. The payload that goes is the one you were shown, sent back
with the request, so the two cannot differ. Each send is recorded in the audit
log.

The evidence is the object (truncated at 24 KB), up to 25 events, and up to
120 log lines per container. Secret values are replaced before you see it.

**It can still carry things you did not intend.** A pod spec holds environment
variable names and any literal values written into it, and logs hold whatever
the application prints, which is why the preview shows you the bytes rather
than describing them. If your organisation forbids sending cluster data to a
third party, do not add a key and the feature stays inert.

An earlier version of this page said the evidence was shown by a **Show what
was sent** button. It was, which is to say afterwards. That was the wrong
order and it has been changed.

If `ANTHROPIC_API_KEY` is set in the environment the engine was started in,
the pane uses it. It now says so on screen, because a key you did not type
into ClusterTrail arming a feature silently is not consent.

## Secret values

Kubernetes Secrets are only base64, which is encoding, not encryption. Three
places could leak one, and each is closed deliberately:

The rule is simple: **the owner reading their own cluster sees values; nothing
that leaves this machine does.**

| Path | What happens |
|---|---|
| The interface | Values are masked with a per-key reveal, and resolving one is an explicit click that fetches the Secret at that moment. This is you reading your own cluster, so it is not redacted. |
| Drift capture into Git | Keys are kept so the declaration stays reviewable; every value is replaced with a placeholder, and the file and commit message say so. Writing real values needs an explicit opt-in. |
| The MCP server | Every object a tool returns goes through the redacting renderer. `describe_containers` names the Secret and key behind a variable without resolving it, and `get_resource` on a Secret returns its keys with placeholders. |
| A drift report | The same, on screen, in `--json`, and in whatever CI log that output lands in. |
| The explain pane | The same, before the preview is drawn, so the values are already gone when you decide whether to send. |

The placeholder is a fingerprint rather than a constant, computed with a key
generated fresh each time the engine starts. Two identical values fingerprint
alike within one run, so a Secret whose value drifted still shows as drift;
nothing about the value survives the run, and a short or guessable password
cannot be recovered from it.

The `kubectl.kubernetes.io/last-applied-configuration` annotation is stripped
on the same paths. For anything applied with `kubectl apply` it carries a
second, complete copy of the object, Secret data included, so redacting
`data` while leaving the annotation redacts nothing.

An earlier version of this page argued that `get_resource` returning base64
values was deliberate, on the grounds that a tool which quietly returns
something other than what was asked for is worse. That was a rationalisation.
The tool now says in its own description that values are replaced, which
answers the objection without handing a credential to a model.

Secrets that Kubernetes or Helm generate, service-account tokens and release
records, are not reported as drift. A Secret a person created by hand is,
because that is the one nobody remembers.

If your environment forbids reading Secrets at all, give ClusterTrail a
kubeconfig whose credentials cannot. It inherits RBAC, so the restriction
holds everywhere.

## The audit chain

Each entry is a JSON line:

```json
{"seq":35,"at":"2026-09-18T15:12:03Z","actor":"operator","action":"change.applied",
 "changeId":"chg_c5b75b78596b","cluster":"kind-clustertrail","detail":{...},
 "prev":"babc786091...","hash":"69762bd659..."}
```

`hash` is the SHA-256 of the entry with the hash field blanked, and includes
`prev`. Editing or removing an entry breaks every link after it, and the
engine can walk the chain and name the first broken sequence number.

This is tamper **evidence**, not tamper proofing: someone with write access to
the file could rewrite the whole chain. It raises the cost of quiet edits and
makes a later comparison against a colleague's copy meaningful, which is what
the team tier is designed around.

## What an audit found

Before the first release, this codebase was reviewed with Cloudflare's
security-audit skill: five independent passes over the engine, covering the
local HTTP and WebSocket surface, the change gate and the audit log, drift
and capture-into-Git, the surface a model reads through, and the release
path.

The findings were not in the plumbing. They were in the three things this
page claims: that Secret values do not leave your machine, that the reviewer
sees what will actually happen, and that a model cannot write. Each was true
of the intent and false of the code in at least one place. All of them were
fixed, and each has a regression test, before anything was published for
anyone to run.

That is the reason this page can be specific about what the engine does. The
claims above are not aspirations that survived an audit untested; they are
what is left after one went looking.

## Reviewing it yourself

The properties above are small and local, which is the point:

| Claim | Where to check |
|---|---|
| Binds loopback only | `engine/cmd/clustertrail/main.go` |
| Token required, constant-time | `engine/internal/api/server.go`, `authorized` |
| One mutation path | `engine/internal/change/change.go`, `execute` |
| Diff is a real dry run | same file, `dryRun` |
| Audit chain | `engine/internal/audit/audit.go` |
| Model gets no tools | `engine/internal/explain/explain.go`, `Ask` |

# Features

Everything ClusterTrail does today, screen by screen. Each section says what it is
for, how it works underneath, and where its limits are.

---

## What changed

The first question of any incident is never "show me pods", it is "what
changed in the last hour". That answer normally lives in four places nobody
looks at together. This screen merges them into one ordered list:

| Source | Where it comes from | What it tells you |
|---|---|---|
| **Rollouts** | ReplicaSet revisions, compared pairwise | That a Deployment rolled, which revision, and the image that actually changed between them. |
| **Field writes** | `metadata.managedFields` | Which manager wrote which fields, and when. This is the closest thing Kubernetes has to a built-in change log, it is on every object, and almost no tool surfaces it. |
| **Cluster events** | The events API | What Kubernetes did to itself, and what broke. |
| **Your changes** | ClusterTrail's own audit log | What a person did through the change gate, including rejections. |

Because `managedFields` records the field manager, the timeline can name the
actor. A line reads `Deployment/catalog update spec.template.spec [kubectl-set]`,
which tells you a person ran `kubectl set image`, not that a controller
reconciled something.

### Causes, not consequences

A rollout produces twenty kubelet events. Showing them all buries the one line
that matters, so the timeline keeps warnings, scaling decisions, evictions and
node changes, and drops the `Created` and `Started` chatter that is merely the
consequence of a change already listed.

### Only what a person did

One checkbox filters to human action: field writes by a manager that is not a
controller, plus ClusterTrail's own audit entries. On a busy cluster this collapses
a hundred lines to three, and it is the fastest way to answer "did someone
touch this?"

Windows are 30 minutes, 3 hours or 12 hours. Clicking a row opens that object.

---

## The resource tables

**Twenty-one built-in kinds**, every one live through a Kubernetes informer:

| Group | Kinds |
|---|---|
| Workloads | Pods, Deployments, StatefulSets, DaemonSets, ReplicaSets, Jobs, CronJobs |
| Config | ConfigMaps, Secrets |
| Network | Services, Ingresses |
| Storage | PersistentVolumeClaims |
| Access | ServiceAccounts, Roles, RoleBindings, ClusterRoles, ClusterRoleBindings |
| Cluster | Nodes, Namespaces, Events, CustomResourceDefinitions |

Plus **any custom resource**: open Custom Resources, click a CRD, and ClusterTrail
browses that resource using the printer columns the CRD itself declares, so
the table matches what `kubectl get` prints.

### Why they stay fast

The engine never sends whole objects to a table. Each subscription carries a
projection function that reduces an object to a row of the handful of fields a
table shows, roughly 200 bytes. Changes are batched and emitted at most every
100 ms. The table is virtualised, so only the rows in view exist in the DOM.
Ten thousand pods cost about what forty do.

Row status reproduces what `kubectl get pods` prints, including the awkward
cases: init-container progress, `CrashLoopBackOff`, `Completed`, `Terminating`
and `Unknown` on a lost node. The table never disagrees with the CLI.

### Opening instantly

The engine keeps the last rows it saw for each table on disk, and paints them
the moment you open a screen, before any cluster has answered. The table is
labelled **as of last session, syncing…** while that is true, and the label
disappears when the informer's own snapshot replaces the rows wholesale.

Measured on a cold engine against the demo cluster:

| First rows on screen | Time |
|---|---|
| From the cache | 8 ms |
| From the cluster | 109 ms |

Cached rows are a picture of the past and are never used to decide anything.
They expire after twelve hours, because a day-old picture of a cluster is
misleading rather than helpful, and they are written atomically so a crash
mid-write cannot leave a file that fails to parse.

### Metrics in the row

When metrics-server is present, CPU and memory arrive in the same row update
as the status. The engine polls `metrics.k8s.io` every 15 seconds and merges
the numbers into the rows. Without metrics-server the columns show a dash and
the pods screen says "no metrics" rather than pretending.

### Sorting, filtering, counts

Click a header to sort; click again to reverse. The filter matches names,
namespaces, statuses and every cell, and takes several terms at once. The left
rail shows a live count next to each kind, in red when something in it is
failing, so you can see that Pods has a problem before you open it.

---

## The overview

The incident starting point. It opens with a sentence about the cluster, not a
row of tiles:

> kind-clustertrail needs attention: 2 pods failing and 2 deployments degraded.

Then four things:

- **Usage**, cluster-wide CPU and memory over the last 5, 15 or 30 minutes,
  drawn from a ring of samples the engine keeps in memory.
- **Top consumers**, the six pods using the most CPU and the most memory.
- **Needs attention**, every degraded deployment, failing pod and unready
  node, each linking straight to the logs or events that explain it.
- **Warnings in the last hour**, the live warning event feed.

### Charts without a monitoring stack

The engine keeps 120 samples per object at 15-second intervals, so half an
hour of history, for every pod, every node, and the cluster as a whole. That
is enough to answer "was it always like this?" during an incident. It is in
memory only and starts empty when the engine restarts. For longer history,
ClusterTrail is designed to read Prometheus rather than become one.

---

## The detail drawer

Click any row. The drawer has up to six tabs.

### Overview

Facts about the object, then the **container detail**: each container with its
image, state, restart count, ports, resource requests and limits, and its
liveness, readiness and startup probes.

Under that, the environment, with the source of every value:

| What you see | What it means |
|---|---|
| `SERVICE_NAME  checkout` | A literal value in the spec. |
| `DB_PASSWORD  ••••••••  [Resolve]` | Comes from a Secret. Press Resolve and ClusterTrail fetches it and decodes the key. |
| `LOG_LEVEL  configmap checkout-settings.LOG_LEVEL  [Resolve]` | Comes from a ConfigMap. |
| `POD_IP  field status.podIP` | The downward API. |
| `all of secret app-credentials` | An `envFrom` block. |

Then mounts, each showing what backs it: `secret checkout-credentials`,
`configmap checkout-settings`, `claim data`, `emptyDir`, `projected`.

Workload kinds get the same section, read from their pod template, so you can
inspect a Deployment's containers without finding one of its pods first.

ConfigMaps and Secrets also list their keys, with Secret values masked behind
a per-key reveal, and a **Used by** section answering the question that
actually matters before you change one: which workloads consume it, and how.
Each entry says whether it arrives as an environment variable, a mount, an
image pull secret or through a service account, names the container and the
key or path, and links to the workload. Grepping manifests misses anything
applied by hand; this walks the live objects instead.

### Events

Every event for this object, newest first, with its reason, message, count and
source. Warnings are marked. Kubernetes discards events after an hour, so a
quiet object is usually a healthy one, and the empty state says so.

### Explain

See [Explain this](#explain-this) below.

### Logs

Follows the log with the last 500 lines as context. You get a container picker
for multi-container pods, a **previous** toggle for reading what the last
container printed before it died, a line filter, word wrap, and pause. Errors
and warnings are tinted. Auto-scroll sticks to the bottom until you scroll up,
then a "jump to live" button appears.

Timestamps come from the API and are shown dimmed beside each line. The engine
batches lines every 100 ms or every 500 lines, so a chatty pod cannot flood the
socket, and it caps the buffer at 5000 lines.

### YAML

The live object in a CodeMirror editor with YAML highlighting, with
`managedFields` removed because nobody reads it. Edit and press Ctrl-S and
ClusterTrail proposes the change: you see the dry-run diff before anything is
written. The editor follows the light and dark theme.

Saving uses the resourceVersion you loaded, so if someone else changed the
object while you were editing, you get a conflict instead of silently
overwriting their work.

### Terminal

A real shell in the container, over the API server's exec endpoint. It picks
`bash` and falls back to `sh`. Resizes follow the pane. Opening one is written
to the audit log, because a shell can do anything.

---

## The change gate

**Every write to a cluster is a Change.** Scaling, restarting, deleting,
editing YAML, creating from a template, reverting drift: all the same record,
all the same review.

A Change carries:

| Field | What it holds |
|---|---|
| `author` | `human`, `agent` or `drift`, with a name. |
| `intent` | Why, in a sentence. |
| `plan` | The exact operations: apply, scale, restart or delete. |
| `diff` | A unified diff from a **server-side dry run**, not a guess. |
| `blast` | Namespaces, kinds, object count, whether Secrets are touched, how many deletes. |
| `status` | `pending` → `approved` → `applied`, or `rejected`, or `failed`. |
| `decidedBy`, `decidedAt`, `note` | Who decided, when, and why. |

### Why the diff is trustworthy

ClusterTrail does not diff text. It sends the object you want to the API server with
`dryRun=All`, gets back the object the server would actually store, and diffs
that against what is live. That is what `kubectl diff` does. It means API
defaulting, admission webhooks and mutating controllers are all accounted for,
so the diff shows only your intent. Server-managed noise is stripped:
`managedFields`, `generation`, `resourceVersion`, `uid`, `creationTimestamp`,
the deployment revision annotation and `last-applied-configuration`.

### The structural guarantee

The gate is enforced in the engine, not in the interface. A Change authored by
an agent is created `pending` and **only a human decision can move it**. The
agent's tool surface contains no operation that mutates a cluster at all. That
is the property a security reviewer can check in about fifty lines of Go, and
it is why the agent can be allowed near production.

### The audit log

Every proposal and every decision appends an entry to a hash-chained,
append-only log at `<data dir>/audit.jsonl`. Each entry carries the hash of
the one before it, so removing or editing an entry breaks the chain and can be
detected. Recorded actions today:

`change.proposed`, `change.approved`, `change.rejected`, `change.applied`,
`change.failed`, `exec.opened`.

The Changes screen lists every change with its diff, decision and note, next
to the raw audit chain with its hashes.

---

## Drift

The screen the desktop tools do not have: **what is running that is not in
Git, and what is in Git that is not running.**

Point it at a directory of Kubernetes YAML and press Scan. Every object in
every file is compared against the live cluster, and you get three categories:

| Category | Meaning |
|---|---|
| **in Git, not running** | The file exists, the object does not. |
| **differs from Git** | Both exist and they disagree, with the diff. |
| **running, not in Git** | The object exists with no file behind it. |

Comparison is the same server-side dry run the change gate uses, so defaulted
fields never appear as false drift.

### What counts as unmanaged

Reporting everything in the cluster as unmanaged would be noise. ClusterTrail skips
objects owned by a controller (a ReplicaSet's pods are not unmanaged, they are
managed by their Deployment), system namespaces, and objects Kubernetes puts
in every namespace such as `kube-root-ca.crt`. It looks only at the kinds a
person writes by hand, and only in namespaces the repository actually touches.

### Acting on drift

Nothing on this screen writes to a cluster by itself.

- **in Git, not running** → *Create from Git*, which proposes an apply.
- **differs from Git** → *Revert to Git*, which proposes an apply.
- **running, not in Git** → *Copy YAML for Git* puts a cleaned copy on your
  clipboard to paste into your repository, or *Delete from cluster* proposes a
  delete.

Every one of those goes through the review dialog first.

### Helm charts

A directory containing a `Chart.yaml` is rendered with Helm's own template
engine, the same one `helm template` uses, with the values files beside it
merged in. A chart's templates are not YAML until Helm has been through them,
so comparing them raw would be meaningless.

Rendering is offline. Dependencies already vendored under `charts/` are used;
a chart that would need a repository fetch is reported with the command that
fixes it rather than reaching the network during a drift check.

### Kustomize

A directory containing a `kustomization.yaml` is built with Kustomize before
comparison, exactly as `kubectl kustomize` would, so patches, generators and
name prefixes are all applied first. Comparing an unrendered base against a
cluster built from an overlay would report every patched field as drift
forever, which is worse than not looking.

**Bases are not compared on their own.** A root that another root lists as a
resource is an input, not something deployed, so it is rendered once as part
of the overlay that patches it rather than twice with conflicting values.

### Capturing into Git

When the cluster is right and Git is behind, the drift screen writes the other
way. **Capture into Git** takes every object running with no file behind it,
strips the fields the API server owns, writes one file each into a folder you
name, commits them on a new branch, and opens the pull request page.

Server-owned fields are removed, so each file is a declaration of intent
rather than a snapshot: no `resourceVersion`, no `status`, no `managedFields`,
no assigned cluster IP. Committing those guarantees a conflict on the next
apply.

**Secret values are never written.** A Secret's keys are kept, because the
shape is what a reviewer needs to see, but every value is replaced with a
placeholder and the file carries a comment saying why and what to do instead.
Base64 is not encryption and a repository is not a secret store; making that
mistake one click easy is how credentials end up in breach reports. The
capture reports exactly which keys it redacted, in the interface and in the
commit message.

Secrets that Kubernetes or Helm generate, such as service-account tokens and
Helm release records, are not reported as drift at all. A Secret someone
created by hand during an incident is.

It refuses to run on a repository with uncommitted changes, so the branch
holds only the captured objects and never mixes in your work in progress. If
there is no remote, or the push fails, the branch is still committed locally
and it says so. The capture is recorded in the audit log.

### Limits

Drift reads plain YAML, Kustomize and Helm charts. It does not resolve remote
Kustomize bases or fetch chart dependencies, because a drift check should not
reach the network.

---

## Helm

Releases are read straight from the objects Helm stores in the cluster. Helm
keeps each revision in a Secret whose payload is base64, then gzip, then JSON.
ClusterTrail decodes that directly, which means:

- no Helm binary on your machine,
- no chart repository access,
- no extra dependency in the engine,
- releases installed by any Helm version show up without configuration.

For each release you get the values it was installed with, the manifest it
rendered, its notes, and its revision history with each revision's status and
description.

### Rolling back

Every revision in the history has a **Roll back to this** button. It reads
that revision's rendered manifest, splits it into objects, and proposes
applying them through the same review gate as any other change, so you see the
diff before anything moves.

ClusterTrail is explicit about what this is not. It does **not** rewrite Helm's
release history, so `helm list` still reports the revision Helm thinks is
deployed. The dialog and a notice say so, and point you at `helm rollback`
when you need Helm's own records to match. Reimplementing Helm's release
lifecycle to avoid that caveat would risk corrupting the history it is meant
to maintain, which is a worse trade than stating the limit plainly.

Objects of kinds ClusterTrail cannot apply yet are left out of the plan, and it
tells you how many.

---

## Explain this

A drawer tab that hands one object, its events and its recent log lines to
Claude and shows the answer in prose: what is wrong, the evidence, the likely
cause, and what to check next.

**It is read-only by construction.** The model is given text and returns text.
It has no tools, no cluster access, and no way to act. Anything it suggests is
something you then choose to do through the change gate. A **Show what was
sent** button prints the exact evidence, so the answer is never a black box.

Bring your own key. It is held in the engine's memory for the session, never
written to disk, and never appears in the audit log. You can also start the
engine with `ANTHROPIC_API_KEY` set. The model is `claude-opus-5`.

---

## Creating objects

Every table has a **New** button. It opens a YAML template for that kind,
pre-filled with the current namespace and sensible defaults, in the same
editor used for editing. Templates ship for Namespace, Deployment, Service,
ConfigMap, Secret, Job, CronJob, Ingress, PersistentVolumeClaim, StatefulSet
and DaemonSet.

Saving proposes a Change, so you see the dry-run result before the object
exists.

---

## Port forwarding

Forward a port from the drawer. Forwards are owned by the engine, not the
browser tab, so reloading the UI does not drop them; they die with the engine.
Active forwards are listed in the left rail with a clickable local URL and a
stop button. Leave the local port at zero and the operating system picks a
free one.

---

## Command palette

`Ctrl/⌘ K`. Fuzzy-matches screens, clusters, namespaces and every object in
the table you are looking at. Arrow keys move, Enter opens, Escape closes.

---

## Themes

Light and dark, following your system by default, switchable from the bottom
of the rail. The choice is remembered per browser. The editor and terminal
follow it too, rather than staying dark on a light page.

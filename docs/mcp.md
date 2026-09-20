# The MCP server

```
clustertrail mcp [--kubeconfig PATH]
```

A Model Context Protocol server over stdio that gives a coding agent read
access to the clusters in your kubeconfig.

## The point is what it cannot do

There is no apply, no patch, no delete, no scale and no exec. Every tool
reads. An agent wired to ClusterTrail can understand a cluster in detail and cannot
change it.

That is the only shape a security team approves, and it is the same argument
the desktop client makes: the interesting property is not that the AI is
clever, it is that the AI is structurally unable to act. When the proposals
engine lands, the single write it gains will be a proposal into the review
queue that a human still has to approve.

## Connecting it

Claude Code:

```sh
claude mcp add clustertrail -- /path/to/clustertrail mcp
```

Anything that speaks MCP over stdio works the same way. For a client
configured with JSON:

```json
{
  "mcpServers": {
    "clustertrail": {
      "command": "/path/to/clustertrail",
      "args": ["mcp"],
      "env": { "KUBECONFIG": "/home/you/.kube/config" }
    }
  }
}
```

## The tools

| Tool | What it returns |
|---|---|
| `list_clusters` | The contexts in the kubeconfig, marking the current one. Nothing connects until another tool names one. |
| `list_kinds` | The kind keys this server understands, including how to name a custom resource. |
| `list_resources` | Objects of one kind as a table, the way `kubectl get` prints them. |
| `get_resource` | One object as YAML, with `managedFields` removed. |
| `get_events` | Recent events for one object, newest first. |
| `get_logs` | The last lines of a pod's logs. |
| `what_changed` | Rollouts, field writes, notable events and ClusterTrail's own changes, merged into one ordered list. |
| `drift_report` | A directory of YAML compared with the cluster, with diffs. |
| `describe_containers` | Each container with its image, ports, resources, probes, the source of every environment variable, and what backs each mount. |

Every tool takes an optional `cluster`; omit it to use the current context.

## What an agent does with this

The tool worth understanding is `what_changed`. Asked "why did checkout start
failing", an agent can read it and get:

```
07:33:36  field    Deployment/catalog  update spec.template.spec  [kubectl-set]
07:33:36  rollout  Deployment/catalog  rolled out revision 2
    image busybox:1.36, nginx:1.27-alpine → busybox:1.36, nginx:1.26-alpine
07:33:45  event    Pod/payments-7fdf75d69-p4b9k  BackOff  [kubelet]
```

That is the causal chain in three lines: a person ran `kubectl set image`, a
rollout followed, and something started crashing. No other Kubernetes MCP
server surfaces the `managedFields` record, which is what names the actor.

## Secrets

No tool returns a Secret value. `describe_containers` names the Secret and
the key behind an environment variable without resolving it, and every object
any other tool returns goes through the redacting renderer: keys stay,
values are replaced, and the `last-applied-configuration` annotation, which
carries a second copy of the whole object for anything applied with `kubectl
apply`, is stripped with them. Each tool's description says so, so nothing is
quietly different from what was asked for.

If it matters in your environment, you can also give the server a kubeconfig
whose credentials cannot read Secrets. It inherits RBAC, so that restriction
holds everywhere.

## What "read-only" means here

It is enforced at the transport, not by each tool remembering. The session
these tools share refuses any request to the API server that is not a read or
an explicit server-side dry run, so the property holds whatever a tool asks
for, including a tool added later.

One tool is not a pure read. `drift_report` takes its diff the way `kubectl
diff` does: a dry-run update, which asks the API server what it *would* store.
That needs `update` permission and it runs mutating and validating admission
webhooks. It persists nothing, and it is the only non-read the transport
allows. Saying "writes nothing" was imprecise, so the tool's own description
now spells this out.

## Cluster text is data, not instructions

Everything a tool returns comes back wrapped in a `<cluster-data>` label.
Anyone who can create an object, an annotation or an event in a cluster you
are watching chooses some of that text, and an unlabelled tool result is hard
for a model to tell from something you said. The label does not make injection
impossible; it makes the provenance explicit, which is the part the server can
honestly do. The rest is the agent's own permissions, which is why the write
path is not in this surface at all.

## Limits

The server connects lazily and holds one session per context for its lifetime.
It has no subscriptions: every call is a fresh read. A very large cluster will
make `list_resources` slow, because it lists rather than paging.

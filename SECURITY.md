# Security policy

ClusterTrail runs on an engineer's own machine, holds a local HTTP server, and
authenticates to Kubernetes clusters with their credentials. A flaw in any of
those is worth reporting, and we will work with you in good faith.

## Reporting a vulnerability

**Please do not open a public issue.**

Use GitHub's private vulnerability reporting: the Security tab, then *Report a
vulnerability*. It gives us a coordinated-disclosure workspace and keeps the
details out of the open until there is a fix.

Please include what you found, how to reproduce it, the impact you believe it
has, and a suggested fix if you have one. A proof of concept helps enormously.

We will acknowledge within a few days, agree a disclosure date with you, and
credit you in the release notes unless you would rather we did not.

## What we consider in scope

The properties below are the ones the design depends on. Anything that breaks
one of them is a vulnerability, not a bug:

| Property | Where it lives |
|---|---|
| The engine listens on loopback only and refuses any other address | `engine/cmd/clustertrail/main.go` |
| Every socket connection requires the per-launch token, compared in constant time | `engine/internal/api/server.go` |
| No cluster write happens outside the change gate | `engine/internal/change/change.go` |
| The audit chain detects a tampered or removed entry | `engine/internal/audit/audit.go` |
| The model is given no tool that can reach a cluster | `engine/internal/explain`, `engine/internal/mcp` |
| The MCP server exposes read-only tools only | `engine/internal/mcp/tools.go` |

Credential handling is also in scope: the model key is held in memory and must
never reach disk, the audit log, or any request other than the model call.

Secret values are in scope too. Drift capture must not write a Secret's values
into a repository unless the caller explicitly asks, and the MCP server's
`describe_containers` must name the Secret and key behind a variable without
returning its value.

## What we consider out of scope

- Anything requiring an attacker who already has your kubeconfig or shell
  access to your machine. At that point they can run `kubectl`.
- The unsigned binaries warning on macOS and Windows. That is a known and
  documented cost, not a flaw.
- Findings that depend on you deliberately pointing the tool at a hostile
  cluster whose API server returns malicious data, unless it escapes the
  process.

## What ClusterTrail deliberately does not do

It installs nothing in your clusters, inherits your RBAC rather than holding
its own credentials, and sends nothing off your machine except the model call
you explicitly enable. If you find behaviour that contradicts any of that,
treat it as a vulnerability and report it.

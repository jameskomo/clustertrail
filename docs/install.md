# Install and first run

ClusterTrail is one file. There is no installer, no Node, no Docker and nothing to
configure. You download it, you run it, your browser opens.

## 1. Download

Take the build for your machine from the
[latest release](https://github.com/jameskomo/clustertrail/releases/latest):

| Your machine | File |
|---|---|
| macOS, Apple silicon (M1 and later) | `clustertrail-macos-apple-silicon` |
| macOS, Intel | `clustertrail-macos-intel` |
| Linux, 64-bit | `clustertrail-linux-x86_64` |
| Linux, ARM | `clustertrail-linux-arm64` |
| Windows, 64-bit | `clustertrail-windows-x86_64.exe` |

If you are not sure which Mac you have, click the Apple menu, then About This
Mac. "Apple M1", "M2", "M3" or "M4" means Apple silicon.

Each release also has `checksums.txt` if you want to verify the download:

```sh
sha256sum -c checksums.txt --ignore-missing
```

## 2. Run it

### macOS and Linux

```sh
chmod +x clustertrail-*
./clustertrail-macos-apple-silicon serve --open
```

### Windows

Double-click the `.exe`, or from PowerShell:

```powershell
.\clustertrail-windows-x86_64.exe serve --open
```

Your browser opens on ClusterTrail. If it does not, the terminal prints the address
to click, which looks like `http://127.0.0.1:52341/?token=…`.

Leave that terminal window open. Closing it stops ClusterTrail.

## 3. Getting past the security warning

These binaries are not code-signed, which costs money we have not spent yet.
Your operating system will say so the first time. This is expected, and it is
two clicks.

**macOS.** You will see "Apple could not verify ClusterTrail is free of malware".

1. Open **System Settings**, then **Privacy & Security**.
2. Scroll down. There is a line about ClusterTrail being blocked, with an
   **Open Anyway** button. Click it.
3. Run it again and choose **Open**.

Or, if you prefer the terminal, remove the download quarantine flag yourself:

```sh
xattr -d com.apple.quarantine clustertrail-macos-apple-silicon
```

**Windows.** SmartScreen says "Windows protected your PC".

1. Click **More info**.
2. Click **Run anyway**.

**Linux.** Nothing to do.

## 4. What it can see

ClusterTrail reads the kubeconfig you already have, the same file `kubectl` uses,
from `$KUBECONFIG` or `~/.kube/config`. It authenticates as you, including
the sign-in plugins that EKS, GKE and AKS use.

**It can only see and do what your own credentials allow.** Nothing is
installed in your clusters, no agent, no operator, no CRD. If your account
cannot read Secrets in a namespace, neither can ClusterTrail. The security review is
the same one you already did for `kubectl`.

**Nothing leaves your machine.** The engine listens on your own computer only,
on a random port, and requires a token that changes every launch. The one
exception is the Explain feature, which is off until you add a model key, and
which says exactly what it sends before it sends it.

Every context in your kubeconfig appears in the cluster picker. **Nothing
connects until you select it**, so starting ClusterTrail never triggers a cloud
sign-in prompt you did not ask for.

## 5. The first ten minutes

**Start on Overview.** It opens with a sentence about the cluster, then live
CPU and memory, the biggest consumers, whatever needs attention, and the last
hour of warnings.

**Click something red.** The panel opens on that object's logs. If it is a
crashed container, tick **previous** to read what it printed before it died.

**Open a pod and scroll down the Overview tab.** Every container with its
image, ports, resources and probes. Under that, every environment variable
with where its value comes from. A value from a Secret shows as dots with a
**Resolve** button, and mounts say what backs them.

**Try changing something.** Go to Deployments, pick one, type a number in the
scale box and press Scale. You get a dialog that says what will happen in a
sentence, what it touches, and the exact diff, taken from a dry run against
your cluster. Nothing has been written yet. Approve it or press Escape.

**Look at Changes.** Every decision you just made, with its diff, next to an
audit log where each entry carries the fingerprint of the one before it.

**Look at What changed.** Rollouts, the fields people edited, cluster events
and your own changes, in one list. Tick **only what a person did** to see just
the human actions.

**Point Drift at a repository.** Give it the folder your cluster is deployed
from and it tells you what is in Git but not running, what differs, and what
is running with no file behind it.

## 6. Everyday use

| Key | Does |
|---|---|
| `Ctrl/⌘ K` | Search screens, clusters, namespaces and objects |
| `/` | Jump to the filter box |
| `Esc` | Close the panel, then clear the filter |
| `Ctrl/⌘ Enter` | Approve the change you are reviewing |

ClusterTrail remembers the rows it last saw, so the next launch paints a table in
about ten milliseconds and then refreshes it. While it is showing you the
previous session's rows it says so.

Its own files live in your user configuration directory,
`~/.config/clustertrail` on Linux, and hold the audit log and those cached rows.
Nothing else is stored.

## 7. If something goes wrong

The [troubleshooting guide](troubleshooting.md) covers the problems people
actually hit. The most common two:

- **A cluster is red, or namespaces never load.** ClusterTrail could not
  authenticate. Try `kubectl get ns --context <name>`; if that fails too, it is
  your credentials or your VPN, not ClusterTrail.
- **The CPU column is empty.** That cluster has no metrics-server. ClusterTrail says
  so rather than showing zeroes.

## 8. Running it from source

If you would rather build it yourself, or you want to change it, see
[getting started for developers](getting-started.md). You need Go, Node and
kind, and `make bundle` produces exactly the binary described here.

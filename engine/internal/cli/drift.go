// Package cli holds the non-desktop entry points. The engine already knows how
// to compare a repository with a cluster, so exposing that as a command costs
// almost nothing and puts ClusterTrail in a pipeline, where no desktop client goes.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/drift"
)

// Drift runs one comparison and prints it. It exits non-zero when the cluster
// and the repository disagree, so a CI job fails on drift without any glue.
func Drift(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", ".", "directory of Kubernetes YAML to compare")
	context_ := fs.String("cluster", "", "kubeconfig context (default: the current context)")
	namespace := fs.String("namespace", "", "restrict to one namespace")
	kubeconfig := fs.String("kubeconfig", "", "explicit kubeconfig path")
	unmanaged := fs.Bool("unmanaged", false, "also report objects running with no file behind them")
	asJSON := fs.Bool("json", false, "print the full report as JSON")
	quiet := fs.Bool("quiet", false, "print nothing; use the exit code")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: clustertrail drift --repo DIR [--cluster CONTEXT] [--namespace NS] [--unmanaged] [--json]")
		fmt.Fprintln(stderr, "\nExits 0 when the cluster matches the repository, 1 when it does not, 2 on error.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	reg, err := cluster.Load(*kubeconfig)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	defer reg.Close()

	name := *context_
	if name == "" {
		for _, c := range reg.Clusters() {
			if c.Current {
				name = c.Name
			}
		}
		if name == "" {
			fmt.Fprintln(stderr, "error: no current context in the kubeconfig; pass --cluster")
			return 2
		}
	}
	sess, err := reg.Session(name)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}

	rep, err := drift.Scan(context.Background(), sess, *repo, *namespace, *unmanaged)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}

	drifted := rep.Summary.Missing + rep.Summary.Modified + rep.Summary.Unmanaged
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else if !*quiet {
		printReport(stdout, rep, name)
	}
	if drifted > 0 {
		return 1
	}
	return 0
}

func printReport(w io.Writer, rep *drift.Report, clusterName string) {
	fmt.Fprintf(w, "%s vs %s — %d files\n", clusterName, rep.Path, rep.Files)
	if rep.Summary.Missing+rep.Summary.Modified+rep.Summary.Unmanaged == 0 {
		fmt.Fprintf(w, "\nIn sync. %d objects checked.\n", rep.Summary.InSync)
		return
	}
	group := func(state drift.State, heading string) {
		var lines []string
		for _, i := range rep.Items {
			if i.State != state {
				continue
			}
			line := fmt.Sprintf("  %s/%s", i.KindName, i.Name)
			if i.Namespace != "" {
				line = fmt.Sprintf("  %s %s/%s", i.Namespace, i.KindName, i.Name)
			}
			if i.File != "" {
				line += "  (" + i.File + ")"
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s (%d)\n%s\n", heading, len(lines), strings.Join(lines, "\n"))
	}
	group(drift.Missing, "In the repository, not in the cluster")
	group(drift.Modified, "Different in the cluster")
	group(drift.Unmanaged, "In the cluster, not in the repository")
	fmt.Fprintf(w, "\n%d in sync. Run with --json for the diffs.\n", rep.Summary.InSync)
}

var _ = os.Exit

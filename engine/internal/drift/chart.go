package drift

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/getter"

	"github.com/jameskomo/clustertrail/engine/internal/kinds"
)

// isChart reports whether a directory is a Helm chart root.
func isChart(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "Chart.yaml"))
	return err == nil && !info.IsDir()
}

// chartRoots finds every chart under a directory without descending into one.
// A chart owns its subcharts, so walking into `charts/` would compare
// dependencies that the parent has already rendered with its own values.
func chartRoots(root string) ([]string, error) {
	var roots []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git", "node_modules", "vendor":
			return filepath.SkipDir
		}
		if isChart(path) {
			roots = append(roots, path)
			return filepath.SkipDir
		}
		return nil
	})
	return roots, err
}

// valuesFor collects the values files a person would pass on the command line.
// Anything named values.yaml, values-<something>.yaml or <something>.values.yaml
// beside the chart is used, in sorted order, so an environment overlay checked
// in next to the chart is honoured rather than ignored.
func valuesFor(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		ext := strings.ToLower(filepath.Ext(n))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		base := strings.TrimSuffix(n, filepath.Ext(n))
		if base == "values" || strings.HasPrefix(base, "values-") || strings.HasSuffix(base, ".values") {
			files = append(files, filepath.Join(dir, n))
		}
	}
	return files
}

// buildChart renders a chart the way `helm template` does: the same engine,
// the same values merging, the same release context. Comparing a chart's
// unrendered templates against a cluster would be meaningless, because a
// template is not YAML until it has been through Helm.
//
// It renders offline. Dependencies already vendored under charts/ are used;
// anything that would need a repository is reported rather than fetched,
// because a drift check must not reach the network.
func buildChart(root, dir, namespace string) ([]desired, error) {
	// Helm's loader follows symlinks on purpose, so a chart can pull a file
	// from anywhere the user can read into .Files and render it out again.
	if err := noEscapingSymlinks(root, dir); err != nil {
		return nil, err
	}
	ch, err := loader.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("load chart %s: %w", dir, err)
	}
	if err := ch.Validate(); err != nil {
		return nil, fmt.Errorf("chart %s: %w", dir, err)
	}
	if req := ch.Metadata.Dependencies; len(req) > 0 {
		if err := action.CheckDependencies(ch, req); err != nil {
			return nil, fmt.Errorf("chart %s: %w; run `helm dependency build` first", dir, err)
		}
	}

	// Helm's getters need real settings; passing nil panics inside the plugin
	// scan. These settings are only used to locate values files on disk,
	// because a drift check never reaches a chart repository.
	opts := values.Options{ValueFiles: valuesFor(dir)}
	vals, err := opts.MergeValues(getter.All(cli.New()))
	if err != nil {
		return nil, fmt.Errorf("chart %s values: %w", dir, err)
	}

	ns := namespace
	if ns == "" {
		ns = "default"
	}
	rendered, err := chartutil.ToRenderValues(ch, vals, chartutil.ReleaseOptions{
		Name:      ch.Name(),
		Namespace: ns,
		IsInstall: true,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("chart %s: %w", dir, err)
	}
	files, err := engine.Render(ch, rendered)
	if err != nil {
		return nil, fmt.Errorf("render chart %s: %w", dir, err)
	}

	rel, _ := filepath.Rel(root, dir)
	if rel == "." || rel == "" {
		rel = filepath.Base(dir)
	}

	var out []desired
	for name, body := range files {
		// Helm emits partials and NOTES.txt alongside manifests; neither is an
		// object, and an empty render is how a chart says "not enabled".
		if strings.HasSuffix(name, "NOTES.txt") || strings.TrimSpace(body) == "" {
			continue
		}
		for _, doc := range splitDocuments(body) {
			if strings.TrimSpace(doc) == "" {
				continue
			}
			u, ok := decodeObject([]byte(doc))
			if !ok {
				continue
			}
			k, known := kinds.ByKindName(u.GetKind())
			if !known {
				continue
			}
			if u.GetNamespace() == "" && k.Namespaced {
				u.SetNamespace(ns)
			}
			out = append(out, desired{obj: u, file: rel + " (helm)", kind: k})
		}
	}
	return out, nil
}

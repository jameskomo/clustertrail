package drift

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/yaml"

	"github.com/jameskomo/clustertrail/engine/internal/kinds"
)

// decodeObject parses one YAML document into an unstructured object.
//
// It goes through JSON and the Kubernetes decoder rather than straight into a
// map, because a plain YAML unmarshal yields float64 for every number while
// the rest of Kubernetes expects int64. A replica count that arrives as 2.0
// compares and serialises differently from one that arrives as 2.
func decodeObject(body []byte) (*unstructured.Unstructured, bool) {
	asJSON, err := yaml.YAMLToJSON(body)
	if err != nil {
		return nil, false
	}
	u := &unstructured.Unstructured{}
	if err := u.UnmarshalJSON(asJSON); err != nil {
		return nil, false
	}
	if u.GetKind() == "" || u.GetName() == "" {
		return nil, false
	}
	return u, true
}

// kustomizationNames are the file names Kustomize recognises, in its own
// order of preference.
var kustomizationNames = []string{"kustomization.yaml", "kustomization.yml", "Kustomization"}

// isKustomization reports whether a directory is a Kustomize root.
func isKustomization(dir string) bool {
	for _, name := range kustomizationNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// buildKustomize renders an overlay the way `kubectl kustomize` does, so
// patches, generators and name prefixes are all applied before comparison.
// Comparing raw base YAML against a cluster built from an overlay would report
// every patched field as drift, which is worse than not looking at all.
func buildKustomize(root, dir string) ([]desired, error) {
	if err := localRefsOnly(dir); err != nil {
		return nil, err
	}
	if err := noEscapingSymlinks(root, dir); err != nil {
		return nil, err
	}
	opts := krusty.MakeDefaultOptions()
	k := krusty.MakeKustomizer(opts)
	res, err := k.Run(filesys.MakeFsOnDisk(), dir)
	if err != nil {
		return nil, fmt.Errorf("kustomize %s: %w", dir, err)
	}
	rel, _ := filepath.Rel(root, dir)
	if rel == "." || rel == "" {
		rel = filepath.Base(dir)
	}

	var out []desired
	for _, r := range res.Resources() {
		body, err := r.AsYAML()
		if err != nil {
			continue
		}
		u, ok := decodeObject(body)
		if !ok {
			continue
		}
		kind, ok := kinds.ByKindName(u.GetKind())
		if !ok {
			continue
		}
		out = append(out, desired{obj: u, file: rel + " (kustomize)", kind: kind})
	}
	return out, nil
}

// kustomizeRoots finds every Kustomize root under a directory, without
// descending into one. A root owns everything beneath it: walking into an
// overlay's bases would compare unpatched copies of objects the overlay has
// already rendered.
func kustomizeRoots(root string) ([]string, error) {
	var roots []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		base := d.Name()
		if base == ".git" || base == "node_modules" || base == "vendor" {
			return filepath.SkipDir
		}
		if isKustomization(path) {
			roots = append(roots, path)
			return filepath.SkipDir
		}
		return nil
	})
	return roots, err
}

// dropBases removes roots that another root builds on.
//
// A Kustomize base is an input, not something deployed on its own. Rendering
// both a base and the overlay that patches it reports the same object twice,
// once with the patch and once without, and the unpatched copy is always
// wrong.
func dropBases(roots []string) []string {
	base := map[string]bool{}
	for _, dir := range roots {
		for _, ref := range kustomizeResources(dir) {
			resolved := ref
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(dir, ref)
			}
			if clean, err := filepath.Abs(filepath.Clean(resolved)); err == nil {
				base[clean] = true
			}
		}
	}
	out := make([]string, 0, len(roots))
	for _, dir := range roots {
		abs, err := filepath.Abs(dir)
		if err == nil && base[abs] {
			continue
		}
		out = append(out, dir)
	}
	return out
}

// kustomizeResources reads the resource references out of a kustomization
// file. Only local directory references matter here; remote references and
// plain files are not roots.
func kustomizeResources(dir string) []string {
	for _, name := range kustomizationNames {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var k struct {
			Resources []string `json:"resources"`
			Bases     []string `json:"bases"` // the older field, still seen in real repositories
		}
		if err := yaml.Unmarshal(body, &k); err != nil {
			return nil
		}
		refs := append(k.Resources, k.Bases...)
		out := make([]string, 0, len(refs))
		for _, r := range refs {
			if strings.Contains(r, "://") || strings.HasPrefix(r, "git@") {
				continue // a remote base is not a directory on disk
			}
			if info, err := os.Stat(filepath.Join(dir, r)); err == nil && info.IsDir() {
				out = append(out, r)
			}
		}
		return out
	}
	return nil
}

// localRefsOnly refuses a kustomization that names a remote resource.
//
// Kustomize will happily fetch an https URL or clone a git repository to
// resolve one, and its load restrictions do not cover that: the remote branch
// is taken before the restriction is consulted. A drift check reads a
// directory someone pointed us at, which may be a clone of something they do
// not control, and it must not turn reading that directory into an outbound
// request, an SSRF against whatever the workstation can reach, or a git
// subprocess aimed at an attacker's URL.
func localRefsOnly(dir string) error {
	seen := map[string]bool{}
	var walk func(string) error
	walk = func(d string) error {
		abs, err := filepath.Abs(d)
		if err != nil || seen[abs] {
			return err
		}
		seen[abs] = true
		body, ok := readKustomization(d)
		if !ok {
			return nil
		}
		var k struct {
			Resources  []string `json:"resources"`
			Bases      []string `json:"bases"`
			Components []string `json:"components"`
			Crds       []string `json:"crds"`
		}
		if err := yaml.Unmarshal(body, &k); err != nil {
			return nil
		}
		for _, r := range append(append(append([]string{}, k.Resources...), k.Bases...), append(k.Components, k.Crds...)...) {
			// A ref that resolves on disk is local, whatever it looks like.
			// Checking that first keeps a directory named "base.v2" from
			// being mistaken for a hostname.
			next := filepath.Join(d, r)
			if info, err := os.Stat(next); err == nil {
				if info.IsDir() {
					if err := walk(next); err != nil {
						return err
					}
				}
				continue
			}
			if isRemoteRef(r) {
				return fmt.Errorf("kustomization in %s refers to %q, which kustomize would fetch over the network; drift reads only what is already on disk", d, r)
			}
		}
		return nil
	}
	return walk(dir)
}

// isRemoteRef covers the three shapes kustomize accepts: a URL, an scp-style
// git address, and a bare host path such as github.com/org/repo//overlay.
func isRemoteRef(r string) bool {
	r = strings.TrimSpace(r)
	switch {
	case strings.Contains(r, "://"), strings.HasPrefix(r, "git@"):
		return true
	case strings.Contains(r, "//") && !strings.HasPrefix(r, "/"):
		return true
	}
	host, _, found := strings.Cut(r, "/")
	return found && strings.Contains(host, ".") && !strings.HasPrefix(r, ".")
}

func readKustomization(dir string) ([]byte, bool) {
	for _, name := range kustomizationNames {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return b, true
		}
	}
	return nil, false
}

// noEscapingSymlinks refuses a tree that links outside the repository.
//
// Helm's chart loader follows symlinks deliberately, and a chart that links
// to ~/.ssh can read it back out through .Files.Get and render it into an
// object that then appears in the report and, if captured, in a commit.
func noEscapingSymlinks(root, dir string) error {
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("%s is a symlink that cannot be resolved: %w", path, err)
		}
		if target != base && !strings.HasPrefix(target, base+string(os.PathSeparator)) {
			return fmt.Errorf("%s links to %s, outside the repository; drift reads only what the repository contains", path, target)
		}
		return nil
	})
}

// outsideOf drops paths that sit inside one of the given directories.
func outsideOf(paths, dirs []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if !underAny(p, dirs) {
			out = append(out, p)
		}
	}
	return out
}

// underAny reports whether path sits inside one of the given directories.
func underAny(path string, dirs []string) bool {
	for _, d := range dirs {
		if path == d || strings.HasPrefix(path, d+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

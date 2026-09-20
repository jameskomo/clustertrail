// Package drift answers the question the desktop tools do not ask: what is
// running that is not in Git, and what is in Git that is not running.
//
// Desired state is rendered from a directory of YAML. Each object is compared
// with the cluster by a server-side dry-run apply, which is what `kubectl
// diff` does, so API defaulting does not show up as drift.
package drift

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
	"github.com/jameskomo/clustertrail/engine/internal/watch"
)

// State is what a single object's comparison concluded.
type State string

const (
	// Missing: Git has it, the cluster does not.
	Missing State = "missing"
	// Modified: both have it and they differ.
	Modified State = "modified"
	// InSync: both have it and they agree.
	InSync State = "in-sync"
	// Unmanaged: the cluster has it, Git does not.
	Unmanaged State = "unmanaged"
)

// Item is one object's result.
type Item struct {
	State     State  `json:"state"`
	Kind      string `json:"kind"`     // registry key, for the Change gate
	KindName  string `json:"kindName"` // API kind, for display
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	File      string `json:"file,omitempty"`
	Diff      string `json:"diff,omitempty"`
	YAML      string `json:"yaml,omitempty"` // desired, for missing and modified
	Error     string `json:"error,omitempty"`
}

// Report is one run over one repository path.
type Report struct {
	Path      string    `json:"path"`
	Cluster   string    `json:"cluster"`
	ScannedAt time.Time `json:"scannedAt"`
	Files     int       `json:"files"`
	Items     []Item    `json:"items"`
	Summary   struct {
		Missing   int `json:"missing"`
		Modified  int `json:"modified"`
		InSync    int `json:"inSync"`
		Unmanaged int `json:"unmanaged"`
	} `json:"summary"`
}

// systemNamespaces never count as unmanaged: nobody keeps them in an app repo.
var systemNamespaces = map[string]bool{
	"kube-system": true, "kube-public": true, "kube-node-lease": true, "local-path-storage": true,
}

// unmanagedKinds is the set worth reporting as drift. Pods and ReplicaSets are
// created by controllers, so they are owned, not unmanaged.
//
// Secrets are included on purpose. A Secret nobody declared is exactly the
// kind of thing that gets created by hand during an incident and then
// forgotten, and hiding it would be the wrong kind of safety. Capturing one
// into Git strips its values; see stripSecretData.
var unmanagedKinds = []string{"deployments", "statefulsets", "daemonsets", "cronjobs", "services", "ingresses", "configmaps", "secrets", "persistentvolumeclaims"}

type desired struct {
	obj  *unstructured.Unstructured
	file string
	kind kinds.Kind
}

// render turns a directory into the objects it declares.
//
// Kustomize roots are built with Kustomize, so patches and generators are
// applied before anything is compared. Everything else is read as plain YAML.
func render(root, defaultNamespace string) ([]desired, int, error) {
	found, err := kustomizeRoots(root)
	if err != nil {
		return nil, 0, err
	}
	// Bases stay in `found` so the walk still skips their files; only the
	// deployable roots are built.
	roots := dropBases(found)

	charts, err := chartRoots(root)
	if err != nil {
		return nil, 0, err
	}
	// A chart inside a Kustomize root belongs to that root's build.
	charts = outsideOf(charts, found)

	var out []desired
	files := 0
	for _, dir := range roots {
		built, err := buildKustomize(root, dir)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, built...)
		files++
	}
	for _, dir := range charts {
		built, err := buildChart(root, dir, defaultNamespace)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, built...)
		files++
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "node_modules" || base == "vendor" {
				return filepath.SkipDir
			}
			// A Kustomize root has already been rendered as a whole.
			if underAny(path, found) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		rel, _ := filepath.Rel(root, path)
		for _, doc := range splitDocuments(string(raw)) {
			if strings.TrimSpace(doc) == "" {
				continue
			}
			u, ok := decodeObject([]byte(doc))
			if !ok {
				continue // not a Kubernetes object; a values file or a template
			}
			k, ok := kinds.ByKindName(u.GetKind())
			if !ok {
				continue // a kind ClusterTrail does not know yet
			}
			out = append(out, desired{obj: u, file: rel, kind: k})
		}
		return nil
	})
	return out, files, err
}

// splitDocuments splits on a line that is exactly a document separator.
func splitDocuments(s string) []string {
	lines := strings.Split(s, "\n")
	var docs []string
	var cur []string
	for _, l := range lines {
		if strings.TrimRight(l, " \t\r") == "---" {
			docs = append(docs, strings.Join(cur, "\n"))
			cur = nil
			continue
		}
		cur = append(cur, l)
	}
	return append(docs, strings.Join(cur, "\n"))
}

// Scan compares a directory of YAML with a cluster.
func Scan(ctx context.Context, sess *cluster.Session, root, defaultNamespace string, includeUnmanaged bool) (*Report, error) {
	root = strings.TrimSpace(root)
	if strings.HasPrefix(root, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, root[2:])
		}
	}
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		// A path pasted out of prose often carries a trailing period or
		// bracket. Say so rather than making the person spot it.
		if trimmed := strings.TrimRight(root, ".,;:)]}'\""); trimmed != root {
			if _, err2 := os.Stat(trimmed); err2 == nil {
				return nil, fmt.Errorf("no directory at %s. %s does exist, so the path picked up a trailing character", root, trimmed)
			}
		}
		return nil, fmt.Errorf("no directory at %s", root)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is a file, not a directory. Give the folder that holds your YAML", root)
	}
	objs, files, err := render(root, defaultNamespace)
	if err != nil {
		return nil, err
	}

	rep := &Report{Path: root, Cluster: sess.Name, ScannedAt: time.Now().UTC(), Files: files}
	known := map[string]bool{}  // kind/ns/name seen in the repo
	repoNS := map[string]bool{} // namespaces the repo touches

	for _, d := range objs {
		ns := d.obj.GetNamespace()
		if ns == "" && d.kind.Namespaced {
			ns = defaultNamespace
			d.obj.SetNamespace(ns)
		}
		item := Item{Kind: d.kind.Key, KindName: d.kind.Kind, Namespace: ns, Name: d.obj.GetName(), File: d.file}
		known[d.kind.Key+"/"+ns+"/"+d.obj.GetName()] = true
		if ns != "" {
			repoNS[ns] = true
		}
		item.YAML = string(mustYAML(d.obj))

		res := sess.Dynamic.Resource(d.kind.GVR)
		var ri = res.Namespace(ns)
		if !d.kind.Namespaced {
			ri = res.Namespace("")
		}
		live, err := ri.Get(ctx, d.obj.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			item.State = Missing
			rep.Items = append(rep.Items, item)
			rep.Summary.Missing++
			continue
		}
		if err != nil {
			item.State = Modified
			item.Error = err.Error()
			rep.Items = append(rep.Items, item)
			continue
		}
		want := d.obj.DeepCopy()
		want.SetResourceVersion(live.GetResourceVersion())
		after, err := ri.Update(ctx, want, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}, FieldManager: "clustertrail"})
		if err != nil {
			item.State = Modified
			item.Error = err.Error()
			item.Diff = unified(reportYAML(live), reportYAML(want))
			rep.Items = append(rep.Items, item)
			rep.Summary.Modified++
			continue
		}
		before, afterY := reportYAML(live), reportYAML(after)
		if before == afterY {
			item.State = InSync
			rep.Summary.InSync++
		} else {
			item.State = Modified
			item.Diff = unified(before, afterY)
			rep.Summary.Modified++
		}
		rep.Items = append(rep.Items, item)
	}

	if includeUnmanaged {
		for _, key := range unmanagedKinds {
			k, err := kinds.Lookup(key)
			if err != nil {
				continue
			}
			list, err := sess.Dynamic.Resource(k.GVR).Namespace("").List(ctx, metav1.ListOptions{LabelSelector: labels.Everything().String()})
			if err != nil {
				continue
			}
			for i := range list.Items {
				u := &list.Items[i]
				ns := u.GetNamespace()
				if systemNamespaces[ns] || len(u.GetOwnerReferences()) > 0 || clusterManaged(u.GetName()) {
					continue
				}
				if key == "secrets" {
					if t, _, _ := unstructured.NestedString(u.Object, "type"); generatedSecretTypes[t] {
						continue
					}
				}
				// Only namespaces the repository actually manages, else every
				// cluster add-on shows up as drift.
				if defaultNamespace != "" && ns != defaultNamespace {
					continue
				}
				if defaultNamespace == "" && !repoNS[ns] {
					continue
				}
				if known[key+"/"+ns+"/"+u.GetName()] {
					continue
				}
				rep.Items = append(rep.Items, Item{State: Unmanaged, Kind: key, KindName: k.Kind, Namespace: ns, Name: u.GetName(), YAML: reportYAML(u)})
				rep.Summary.Unmanaged++
			}
		}
	}

	order := map[State]int{Missing: 0, Modified: 1, Unmanaged: 2, InSync: 3}
	sort.Slice(rep.Items, func(i, j int) bool {
		if order[rep.Items[i].State] != order[rep.Items[j].State] {
			return order[rep.Items[i].State] < order[rep.Items[j].State]
		}
		return rep.Items[i].Name < rep.Items[j].Name
	})
	return rep, nil
}

// clusterManaged skips objects Kubernetes itself puts in every namespace, plus
// the Secrets its own controllers generate. Reporting a service-account token
// or a Helm release record as drift would bury the objects a person wrote.
func clusterManaged(name string) bool {
	switch {
	case name == "kube-root-ca.crt",
		strings.HasPrefix(name, "default-token-"),
		strings.HasPrefix(name, "sh.helm.release."):
		return true
	}
	return false
}

// generatedSecretTypes are created by Kubernetes or by a package manager, not
// declared by a person, so they are not drift.
var generatedSecretTypes = map[string]bool{
	"kubernetes.io/service-account-token": true,
	"helm.sh/release.v1":                  true,
	"bootstrap.kubernetes.io/token":       true,
}

func mustYAML(u *unstructured.Unstructured) []byte {
	b, _ := yaml.Marshal(u.Object)
	return b
}

// reportYAML renders an object for a drift report. A report travels further
// than the machine that made it: into a CI log, onto a clipboard, into a
// model's context. Secret values are fingerprinted rather than printed, so a
// changed value still shows as drift without the value itself being readable.
func reportYAML(u *unstructured.Unstructured) string {
	c := u.DeepCopy()
	watch.Redact(c)
	return watch.ToDiffYAML(c)
}

func unified(before, after string) string {
	d := difflib.UnifiedDiff{A: difflib.SplitLines(before), B: difflib.SplitLines(after), FromFile: "live", ToFile: "git", Context: 3}
	s, _ := difflib.GetUnifiedDiffString(d)
	return s
}

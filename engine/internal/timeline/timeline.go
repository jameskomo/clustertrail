// Package timeline answers the first question of any incident: what changed?
//
// The answer normally lives in four places nobody looks at together. This
// package merges them into one ordered list:
//
//   - rollouts, reconstructed from ReplicaSet revisions, with the image that
//     actually changed between them;
//   - field writes, read from metadata.managedFields, which records which
//     manager touched which fields and when, and which almost no tool shows;
//   - events, for what Kubernetes did to itself;
//   - ClusterTrail's own audit log, for what a person did through the change gate.
//
// Everything here is read-only.
package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/jameskomo/clustertrail/engine/internal/audit"
	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
)

// Source says which of the four inputs an item came from, so the UI can let
// a person trust or discount it accordingly.
type Source string

const (
	SourceRollout Source = "rollout"
	SourceField   Source = "field"
	SourceEvent   Source = "event"
	SourceOwn     Source = "clustertrail"
)

// Item is one thing that happened.
type Item struct {
	At        time.Time `json:"at"`
	Source    Source    `json:"source"`
	Kind      string    `json:"kind,omitempty"`     // registry key, so the UI can open it
	KindName  string    `json:"kindName,omitempty"` // API kind, for display
	Namespace string    `json:"namespace,omitempty"`
	Name      string    `json:"name,omitempty"`
	Actor     string    `json:"actor,omitempty"`  // field manager, controller, or person
	Summary   string    `json:"summary"`          // one line, already written for a human
	Detail    string    `json:"detail,omitempty"` // the specifics, when there are any
	Severity  string    `json:"severity"`         // info | warn | bad
	Count     int32     `json:"count,omitempty"`
}

// Report is one scan of one window.
type Report struct {
	Cluster   string    `json:"cluster"`
	Namespace string    `json:"namespace,omitempty"`
	Since     time.Time `json:"since"`
	Minutes   int       `json:"minutes"`
	Items     []Item    `json:"items"`
	Counts    struct {
		Rollout int `json:"rollout"`
		Field   int `json:"field"`
		Event   int `json:"event"`
		Own     int `json:"clustertrail"`
	} `json:"counts"`
	Truncated bool `json:"truncated,omitempty"`
}

// fieldKinds are the kinds whose managedFields are worth reading. Pods are
// excluded on purpose: the kubelet rewrites their status constantly and the
// interesting change is always on the thing that owns them.
var fieldKinds = []string{"deployments", "statefulsets", "daemonsets", "configmaps", "secrets", "services", "ingresses", "cronjobs"}

// controllerManagers write fields as part of normal reconciliation. Their
// writes are shown, but marked as machine rather than human, so a person can
// filter to "what did a human do".
var controllerManagers = map[string]bool{
	"kube-controller-manager": true, "kube-scheduler": true, "kubelet": true,
	"deployment-controller": true, "replicaset-controller": true,
	"statefulset-controller": true, "daemonset-controller": true,
	"cronjob-controller": true, "endpoint-controller": true, "job-controller": true,
}

// maxItems keeps one scan bounded on a large cluster.
const maxItems = 400

// Scan gathers everything that changed in the last `minutes`.
func Scan(ctx context.Context, sess *cluster.Session, namespace string, minutes int, log *audit.Log) (*Report, error) {
	if minutes <= 0 {
		minutes = 180
	}
	since := time.Now().Add(-time.Duration(minutes) * time.Minute)
	rep := &Report{Cluster: sess.Name, Namespace: namespace, Since: since.UTC(), Minutes: minutes}

	rep.Items = append(rep.Items, rollouts(ctx, sess, namespace, since)...)
	rep.Items = append(rep.Items, fieldWrites(ctx, sess, namespace, since)...)
	rep.Items = append(rep.Items, events(ctx, sess, namespace, since)...)
	rep.Items = append(rep.Items, ownChanges(sess.Name, since, log)...)

	for _, it := range rep.Items {
		switch it.Source {
		case SourceRollout:
			rep.Counts.Rollout++
		case SourceField:
			rep.Counts.Field++
		case SourceEvent:
			rep.Counts.Event++
		case SourceOwn:
			rep.Counts.Own++
		}
	}
	sort.Slice(rep.Items, func(i, j int) bool { return rep.Items[i].At.After(rep.Items[j].At) })
	if len(rep.Items) > maxItems {
		rep.Items = rep.Items[:maxItems]
		rep.Truncated = true
	}
	return rep, nil
}

// rollouts reconstructs Deployment rollouts from ReplicaSet revisions, and
// reports the image that changed, which is what people actually want to know.
func rollouts(ctx context.Context, sess *cluster.Session, namespace string, since time.Time) []Item {
	k, err := kinds.Lookup("replicasets")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	list, err := sess.Dynamic.Resource(k.GVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	type rs struct {
		revision int
		at       time.Time
		images   []string
		name     string
	}
	byOwner := map[string][]rs{}
	nsOf := map[string]string{}
	for i := range list.Items {
		u := &list.Items[i]
		owner := ""
		for _, o := range u.GetOwnerReferences() {
			if o.Kind == "Deployment" {
				owner = o.Name
			}
		}
		if owner == "" {
			continue
		}
		rev, _ := strconv.Atoi(u.GetAnnotations()["deployment.kubernetes.io/revision"])
		key := u.GetNamespace() + "/" + owner
		nsOf[key] = u.GetNamespace()
		byOwner[key] = append(byOwner[key], rs{revision: rev, at: u.GetCreationTimestamp().Time, images: imagesOf(u), name: u.GetName()})
	}

	var out []Item
	for key, revs := range byOwner {
		sort.Slice(revs, func(i, j int) bool { return revs[i].revision < revs[j].revision })
		for i, r := range revs {
			if r.at.Before(since) {
				continue
			}
			owner := key[strings.Index(key, "/")+1:]
			detail := strings.Join(r.images, ", ")
			if i > 0 {
				if changed := imageDelta(revs[i-1].images, r.images); changed != "" {
					detail = changed
				}
			}
			out = append(out, Item{
				At: r.at, Source: SourceRollout, Kind: "deployments", KindName: "Deployment",
				Namespace: nsOf[key], Name: owner, Actor: "deployment-controller",
				Summary:  fmt.Sprintf("rolled out revision %d", r.revision),
				Detail:   detail,
				Severity: "info",
			})
		}
	}
	return out
}

func imagesOf(u *unstructured.Unstructured) []string {
	cs, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		if m, ok := c.(map[string]any); ok {
			if img, ok := m["image"].(string); ok {
				out = append(out, img)
			}
		}
	}
	sort.Strings(out)
	return out
}

// imageDelta describes the image change between two revisions, if any.
func imageDelta(before, after []string) string {
	if strings.Join(before, ",") == strings.Join(after, ",") {
		return ""
	}
	return fmt.Sprintf("image %s → %s", strings.Join(before, ", "), strings.Join(after, ", "))
}

// fieldWrites reads metadata.managedFields, which records which manager wrote
// which fields and when. It is the closest thing Kubernetes has to a built-in
// change log, and it is on every object.
func fieldWrites(ctx context.Context, sess *cluster.Session, namespace string, since time.Time) []Item {
	var out []Item
	for _, key := range fieldKinds {
		k, err := kinds.Lookup(key)
		if err != nil {
			continue
		}
		lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		list, err := sess.Dynamic.Resource(k.GVR).Namespace(namespace).List(lctx, metav1.ListOptions{})
		cancel()
		if err != nil {
			continue
		}
		for i := range list.Items {
			u := &list.Items[i]
			for _, mf := range u.GetManagedFields() {
				if mf.Time == nil || mf.Time.Time.Before(since) {
					continue
				}
				if mf.Subresource == "status" {
					continue // reconciliation noise, not a change of intent
				}
				paths := fieldPaths(mf.FieldsV1)
				if len(paths) == 0 {
					continue
				}
				machine := controllerManagers[mf.Manager]
				summary := fmt.Sprintf("%s %s", strings.ToLower(string(mf.Operation)), strings.Join(paths, ", "))
				out = append(out, Item{
					At: mf.Time.Time, Source: SourceField, Kind: key, KindName: k.Kind,
					Namespace: u.GetNamespace(), Name: u.GetName(), Actor: mf.Manager,
					Summary:  summary,
					Detail:   machineNote(machine),
					Severity: "info",
				})
			}
		}
	}
	return out
}

func machineNote(machine bool) string {
	if machine {
		return "written by a controller, not a person"
	}
	return ""
}

// fieldPaths turns a fieldsV1 tree into a few readable dotted paths. It stops
// two levels deep, because "spec.template.spec.containers" tells you what you
// need and the full tree does not.
func fieldPaths(f *metav1.FieldsV1) []string {
	if f == nil || len(f.Raw) == 0 {
		return nil
	}
	var tree map[string]any
	if err := json.Unmarshal(f.Raw, &tree); err != nil {
		return nil
	}
	var out []string
	var walk func(node map[string]any, prefix string, depth int)
	walk = func(node map[string]any, prefix string, depth int) {
		for k, v := range node {
			if !strings.HasPrefix(k, "f:") {
				continue
			}
			name := strings.TrimPrefix(k, "f:")
			if name == "metadata" || name == "status" {
				continue
			}
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			child, _ := v.(map[string]any)
			if depth >= 2 || len(child) == 0 {
				out = append(out, path)
				continue
			}
			before := len(out)
			walk(child, path, depth+1)
			if len(out) == before {
				out = append(out, path)
			}
		}
	}
	walk(tree, "", 0)
	sort.Strings(out)
	if len(out) > 4 {
		out = append(out[:4], fmt.Sprintf("and %d more", len(out)-4))
	}
	return out
}

// events reports what Kubernetes did to itself, deduplicated by object and
// reason so a restart loop is one line rather than forty.
func events(ctx context.Context, sess *cluster.Session, namespace string, since time.Time) []Item {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	list, err := sess.Client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	seen := map[string]int{}
	var out []Item
	for i := range list.Items {
		e := &list.Items[i]
		at := e.LastTimestamp.Time
		if at.IsZero() {
			at = e.EventTime.Time
		}
		if at.IsZero() {
			at = e.CreationTimestamp.Time
		}
		if at.Before(since) || !interesting(e) {
			continue
		}
		key := e.InvolvedObject.Namespace + "/" + e.InvolvedObject.Name + "/" + e.Reason
		if idx, ok := seen[key]; ok {
			if at.After(out[idx].At) {
				out[idx].At = at
			}
			continue
		}
		sev := "info"
		if e.Type == corev1.EventTypeWarning {
			sev = "bad"
		}
		count := e.Count
		if count == 0 {
			count = 1
		}
		kindKey := ""
		if k, ok := kinds.ByKindName(e.InvolvedObject.Kind); ok {
			kindKey = k.Key
		}
		seen[key] = len(out)
		out = append(out, Item{
			At: at, Source: SourceEvent, Kind: kindKey, KindName: e.InvolvedObject.Kind,
			Namespace: e.InvolvedObject.Namespace, Name: e.InvolvedObject.Name,
			Actor: e.Source.Component, Summary: e.Reason, Detail: e.Message,
			Severity: sev, Count: count,
		})
	}
	return out
}

// interesting keeps causes and symptoms, and drops consequences.
//
// A rollout already appears as its own item, so the twenty kubelet events it
// produces ("Created", "Started", "SuccessfulCreate") add length without
// adding information. What earns a line is something that broke, something
// that decided to change the world, or something that happened to a node.
func interesting(e *corev1.Event) bool {
	if e.Type == corev1.EventTypeWarning {
		return true
	}
	switch e.Reason {
	case "ScalingReplicaSet", "Killing", "Preempted", "Drain",
		"NodeNotReady", "NodeReady", "NodeAllocatableEnforced", "RegisteredNode":
		return true
	}
	return false
}

// ownChanges reads ClusterTrail's own audit log, so a person's deliberate
// change sits in the same timeline as the cluster's reaction to it.
func ownChanges(clusterName string, since time.Time, log *audit.Log) []Item {
	if log == nil {
		return nil
	}
	entries, err := log.Entries(500)
	if err != nil {
		return nil
	}
	var out []Item
	for _, e := range entries {
		if e.At.Before(since) || (e.Cluster != "" && e.Cluster != clusterName) {
			continue
		}
		sev := "info"
		switch e.Action {
		case "change.failed":
			sev = "bad"
		case "change.rejected":
			sev = "warn"
		case "change.proposed":
			continue // the decision is the event worth showing, not the proposal
		}
		var d struct {
			Intent string `json:"intent"`
			Plan   []struct {
				Op        string `json:"op"`
				Kind      string `json:"kind"`
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"plan"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(e.Detail, &d)
		item := Item{
			At: e.At, Source: SourceOwn, Actor: e.Actor,
			Summary:  strings.TrimPrefix(e.Action, "change.") + ": " + d.Intent,
			Detail:   d.Error,
			Severity: sev,
		}
		if e.Action == "exec.opened" {
			item.Summary = "opened a shell"
		}
		if len(d.Plan) > 0 {
			p := d.Plan[0]
			item.Kind, item.Namespace, item.Name = p.Kind, p.Namespace, p.Name
			if k, err := kinds.Lookup(p.Kind); err == nil {
				item.KindName = k.Kind
			}
		}
		out = append(out, item)
	}
	return out
}

//go:build integration

// Package integration exercises the paths that need a real API server: the
// change gate applying things, drift comparing against live objects, and the
// timeline reading what the cluster recorded.
//
// These tests are the other half of the suite. The unit tests pin the pure
// logic; nothing but a live API server can prove that a dry run returns what
// the server would store, or that managedFields carries the actor.
//
//	go test -tags integration ./internal/integration/
//
// They need a cluster. CI creates one with kind; locally, `make dev` leaves
// one behind. KUBECONFIG and CLUSTERTRAIL_CONTEXT select it.
package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/jameskomo/clustertrail/engine/internal/audit"
	"github.com/jameskomo/clustertrail/engine/internal/change"
	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/drift"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
	"github.com/jameskomo/clustertrail/engine/internal/timeline"
)

const namespace = "clustertrail-test"

func session(t *testing.T) (*cluster.Registry, *cluster.Session) {
	t.Helper()
	reg, err := cluster.Load(os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Skipf("no kubeconfig: %v", err)
	}
	name := os.Getenv("CLUSTERTRAIL_CONTEXT")
	if name == "" {
		for _, c := range reg.Clusters() {
			if c.Current {
				name = c.Name
			}
		}
	}
	if name == "" {
		t.Skip("no current context")
	}
	sess, err := reg.Session(name)
	if err != nil {
		t.Skipf("cannot reach %s: %v", name, err)
	}
	t.Cleanup(reg.Close)
	return reg, sess
}

// namespaceFor creates a throwaway namespace and removes it afterwards, so a
// failed run cannot leave objects behind in someone's cluster.
func namespaceFor(t *testing.T, sess *cluster.Session) string {
	t.Helper()
	ctx := context.Background()
	ns := fmt.Sprintf("%s-%d", namespace, time.Now().UnixNano()%100000)
	k, _ := kinds.Lookup("namespaces")
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": ns},
	}}
	if _, err := sess.Dynamic.Resource(k.GVR).Create(ctx, obj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = sess.Dynamic.Resource(k.GVR).Delete(context.Background(), ns, metav1.DeleteOptions{})
	})
	return ns
}

func gate(t *testing.T, reg *cluster.Registry) *change.Gate {
	t.Helper()
	log, err := audit.Open(t.TempDir() + "/audit.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return change.NewGate(reg, log)
}

func configMap(ns, name, value string) string {
	return fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: %s\ndata:\n  key: %q\n", name, ns, value)
}

func TestProposeWritesNothingUntilApproved(t *testing.T) {
	reg, sess := session(t)
	ns := namespaceFor(t, sess)
	g := gate(t, reg)
	ctx := context.Background()
	k, _ := kinds.Lookup("configmaps")

	plan := []change.Op{{Op: "apply", Kind: "configmaps", Namespace: ns, Name: "settings", YAML: configMap(ns, "settings", "first")}}
	ch, err := g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "create settings", plan)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if ch.Status != "pending" {
		t.Fatalf("a proposal must be pending, got %q", ch.Status)
	}
	// The whole promise of the gate is this assertion.
	if _, err := sess.Dynamic.Resource(k.GVR).Namespace(ns).Get(ctx, "settings", metav1.GetOptions{}); err == nil {
		t.Fatal("proposing must not create the object")
	}
	if !strings.Contains(ch.Diff, "settings") {
		t.Errorf("the diff should describe the object, got:\n%s", ch.Diff)
	}

	if _, err := g.Decide(ctx, ch.ID, "approve", "integration test"); err != nil {
		t.Fatalf("decide: %v", err)
	}
	got, err := sess.Dynamic.Resource(k.GVR).Namespace(ns).Get(ctx, "settings", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("approving should create the object: %v", err)
	}
	data, _, _ := unstructured.NestedString(got.Object, "data", "key")
	if data != "first" {
		t.Errorf("data: want first, got %q", data)
	}
}

func TestRejectingLeavesTheClusterAlone(t *testing.T) {
	reg, sess := session(t)
	ns := namespaceFor(t, sess)
	g := gate(t, reg)
	ctx := context.Background()
	k, _ := kinds.Lookup("configmaps")

	plan := []change.Op{{Op: "apply", Kind: "configmaps", Namespace: ns, Name: "nope", YAML: configMap(ns, "nope", "x")}}
	ch, err := g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "create nope", plan)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	decided, err := g.Decide(ctx, ch.ID, "reject", "not today")
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if decided.Status != "rejected" {
		t.Fatalf("status: want rejected, got %q", decided.Status)
	}
	if _, err := sess.Dynamic.Resource(k.GVR).Namespace(ns).Get(ctx, "nope", metav1.GetOptions{}); err == nil {
		t.Fatal("a rejected change must not reach the cluster")
	}
}

func TestTheDiffComesFromTheServerNotFromText(t *testing.T) {
	reg, sess := session(t)
	ns := namespaceFor(t, sess)
	g := gate(t, reg)
	ctx := context.Background()

	create := []change.Op{{Op: "apply", Kind: "configmaps", Namespace: ns, Name: "settings", YAML: configMap(ns, "settings", "first")}}
	ch, err := g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "create", create)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Decide(ctx, ch.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}

	// Proposing the identical object again must produce no diff, because the
	// server reports it would store the same thing. A text comparison would
	// show spurious differences from defaulting.
	same := []change.Op{{Op: "apply", Kind: "configmaps", Namespace: ns, Name: "settings", YAML: configMap(ns, "settings", "first")}}
	unchanged, err := g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "no change", same)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unchanged.Diff, "no change") {
		t.Errorf("re-applying the same object should report no change, got:\n%s", unchanged.Diff)
	}

	edit := []change.Op{{Op: "apply", Kind: "configmaps", Namespace: ns, Name: "settings", YAML: configMap(ns, "settings", "second")}}
	changed, err := g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "edit", edit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changed.Diff, "second") {
		t.Errorf("a real edit must show in the diff, got:\n%s", changed.Diff)
	}
}

func TestDriftSeesWhatIsMissingModifiedAndUnmanaged(t *testing.T) {
	reg, sess := session(t)
	ns := namespaceFor(t, sess)
	g := gate(t, reg)
	ctx := context.Background()

	// One object exists and matches, one differs, one is only in the repo.
	for name, value := range map[string]string{"matching": "same", "drifted": "cluster-value"} {
		plan := []change.Op{{Op: "apply", Kind: "configmaps", Namespace: ns, Name: name, YAML: configMap(ns, name, value)}}
		ch, err := g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "seed", plan)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Decide(ctx, ch.ID, "approve", ""); err != nil {
			t.Fatal(err)
		}
	}

	repo := t.TempDir()
	for name, value := range map[string]string{"matching": "same", "drifted": "repo-value", "absent": "x"} {
		if err := os.WriteFile(repo+"/"+name+".yaml", []byte(configMap(ns, name, value)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := drift.Scan(ctx, sess, repo, ns, true)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	state := map[string]drift.State{}
	for _, i := range rep.Items {
		state[i.Name] = i.State
	}
	if state["matching"] != drift.InSync {
		t.Errorf("matching: want in-sync, got %q", state["matching"])
	}
	if state["drifted"] != drift.Modified {
		t.Errorf("drifted: want modified, got %q", state["drifted"])
	}
	if state["absent"] != drift.Missing {
		t.Errorf("absent: want missing, got %q", state["absent"])
	}
}

func TestTimelineReadsTheActorFromManagedFields(t *testing.T) {
	reg, sess := session(t)
	ns := namespaceFor(t, sess)
	g := gate(t, reg)
	ctx := context.Background()

	plan := []change.Op{{Op: "apply", Kind: "configmaps", Namespace: ns, Name: "settings", YAML: configMap(ns, "settings", "v1")}}
	ch, err := g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "seed", plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Decide(ctx, ch.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}

	rep, err := timeline.Scan(ctx, sess, ns, 30, nil)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	var found *timeline.Item
	for i := range rep.Items {
		if rep.Items[i].Source == timeline.SourceField && rep.Items[i].Name == "settings" {
			found = &rep.Items[i]
		}
	}
	if found == nil {
		t.Fatalf("the write should appear as a field write; got %d items", len(rep.Items))
	}
	// This is the property the whole timeline rests on: Kubernetes records who
	// wrote which fields, and the field manager is the name we apply.
	if found.Actor != change.FieldManager {
		t.Errorf("actor: want %q, got %q", change.FieldManager, found.Actor)
	}
	if !strings.Contains(found.Summary, "data") {
		t.Errorf("the summary should name the fields written, got %q", found.Summary)
	}
}

func TestScaleAndRestartReachTheCluster(t *testing.T) {
	reg, sess := session(t)
	ns := namespaceFor(t, sess)
	g := gate(t, reg)
	ctx := context.Background()
	k, _ := kinds.Lookup("deployments")

	deployment := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: %s
spec:
  replicas: 1
  selector: { matchLabels: { app: web } }
  template:
    metadata: { labels: { app: web } }
    spec:
      containers:
        - name: web
          image: nginx:1.27-alpine
`, ns)
	create := []change.Op{{Op: "apply", Kind: "deployments", Namespace: ns, Name: "web", YAML: deployment}}
	ch, err := g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "create", create)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Decide(ctx, ch.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}

	three := int32(3)
	scale := []change.Op{{Op: "scale", Kind: "deployments", Namespace: ns, Name: "web", Replicas: &three}}
	ch, err = g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "scale to 3", scale)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ch.Diff, "replicas: 3") {
		t.Errorf("the diff should show the new replica count, got:\n%s", ch.Diff)
	}
	if _, err := g.Decide(ctx, ch.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	got, err := sess.Dynamic.Resource(k.GVR).Namespace(ns).Get(ctx, "web", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n, _, _ := unstructured.NestedInt64(got.Object, "spec", "replicas"); n != 3 {
		t.Errorf("replicas: want 3, got %d", n)
	}

	restart := []change.Op{{Op: "restart", Kind: "deployments", Namespace: ns, Name: "web"}}
	ch, err = g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "restart", restart)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Decide(ctx, ch.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	got, err = sess.Dynamic.Resource(k.GVR).Namespace(ns).Get(ctx, "web", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ann, _, _ := unstructured.NestedStringMap(got.Object, "spec", "template", "metadata", "annotations")
	if ann["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Error("a restart must stamp the pod template, which is what makes pods roll")
	}
}

func TestAnUnknownKindIsRejectedBeforeAnythingIsWritten(t *testing.T) {
	reg, sess := session(t)
	g := gate(t, reg)
	_, err := g.Propose(context.Background(), sess.Name, change.Author{Type: "human", Name: "test"}, "nonsense",
		[]change.Op{{Op: "apply", Kind: "sprockets", Namespace: "default", Name: "a", YAML: "kind: Sprocket\n"}})
	if err == nil {
		t.Fatal("an unknown kind must fail at propose time")
	}
}

// The gate's promise is that what you approve is what happens. If somebody
// else edits the object between the review and the approval, applying the
// reviewed object would silently undo their work, and the diff the approver
// read would no longer describe the result.
//
// The check compares the object as the diff renders it rather than its
// resourceVersion, because a Deployment's version changes every time a
// controller writes status: pinning to that would refuse ordinary approvals
// on any object whose pods were still settling.
func TestAnEditBetweenReviewAndApprovalIsRefused(t *testing.T) {
	reg, sess := session(t)
	ns := namespaceFor(t, sess)
	g := gate(t, reg)
	ctx := context.Background()
	k, _ := kinds.Lookup("configmaps")

	body := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
  namespace: %s
data:
  mode: quiet
`, ns)
	ch, err := g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "create", []change.Op{
		{Op: "apply", Kind: "configmaps", Namespace: ns, Name: "settings", YAML: body},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Decide(ctx, ch.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}

	// Review a change to mode.
	updated := strings.Replace(body, "mode: quiet", "mode: loud", 1)
	ch, err = g.Propose(ctx, sess.Name, change.Author{Type: "human", Name: "test"}, "make it loud", []change.Op{
		{Op: "apply", Kind: "configmaps", Namespace: ns, Name: "settings", YAML: updated},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Somebody else adds a key while the change sits waiting for a decision.
	live, err := sess.Dynamic.Resource(k.GVR).Namespace(ns).Get(ctx, "settings", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(live.Object, "on", "data", "telemetry"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Dynamic.Resource(k.GVR).Namespace(ns).Update(ctx, live, metav1.UpdateOptions{FieldManager: "someone-else"}); err != nil {
		t.Fatal(err)
	}

	decided, err := g.Decide(ctx, ch.ID, "approve", "")
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if decided.Status != "failed" {
		t.Errorf("status: want failed, got %q", decided.Status)
	}
	if !strings.Contains(decided.Error, "changed after this change was reviewed") {
		t.Errorf("the reason should say the object moved, got %q", decided.Error)
	}

	after, err := sess.Dynamic.Resource(k.GVR).Namespace(ns).Get(ctx, "settings", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, _, _ := unstructured.NestedStringMap(after.Object, "data")
	if data["telemetry"] != "on" {
		t.Error("the other person's key was overwritten by a change that never saw it")
	}
	if data["mode"] != "quiet" {
		t.Errorf("nothing should have been applied, got mode=%q", data["mode"])
	}
}

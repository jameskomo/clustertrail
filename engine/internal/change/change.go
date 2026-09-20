// Package change is the one gate every cluster mutation passes through.
//
// A Change is proposed (its diff is computed by server-side dry-run and
// nothing is written), then decided by a human, then applied. A human's own
// change is proposed and approved in two calls from the UI so the person sees
// the diff before every write. An agent can only ever propose.
package change

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/user"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"github.com/jameskomo/clustertrail/engine/internal/audit"
	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
	"github.com/jameskomo/clustertrail/engine/internal/watch"
)

const FieldManager = "clustertrail"

type Op struct {
	Op        string `json:"op"` // apply | scale | restart | delete
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	YAML      string `json:"yaml,omitempty"`
	Replicas  *int32 `json:"replicas,omitempty"`
}

type Author struct {
	Type string `json:"type"` // human | agent | drift
	Name string `json:"name"`
}

type Blast struct {
	Namespaces []string `json:"namespaces"`
	Kinds      []string `json:"kinds"`
	Objects    int      `json:"objects"`
	HasSecrets bool     `json:"hasSecrets"`
	Deletes    int      `json:"deletes"`
}

type Change struct {
	ID        string    `json:"id"`
	Cluster   string    `json:"cluster"`
	Author    Author    `json:"author"`
	Intent    string    `json:"intent"`
	Plan      []Op      `json:"plan"`
	Diff      string    `json:"diff"`
	Blast     Blast     `json:"blast"`
	Status    string    `json:"status"` // pending | approved | rejected | applied | failed
	CreatedAt time.Time `json:"createdAt"`
	DecidedAt time.Time `json:"decidedAt,omitempty"`
	DecidedBy string    `json:"decidedBy,omitempty"`
	Note      string    `json:"note,omitempty"`
	Error     string    `json:"error,omitempty"`

	// reviewed holds, per plan entry, the live object as the diff renders
	// it, captured during the dry run. The apply checks it again before
	// writing, so an edit made by anyone else between the review and the
	// approval is a conflict rather than a silent overwrite.
	//
	// This compares what the diff was built from rather than the object's
	// resourceVersion, which moves every time a controller writes status: a
	// Deployment whose pods are still settling changes version several times
	// a second, and pinning to it would refuse almost every real approval.
	reviewed []string
}

type Gate struct {
	reg   *cluster.Registry
	audit *audit.Log
	user  string

	mu      sync.Mutex
	changes map[string]*Change
}

func NewGate(reg *cluster.Registry, log *audit.Log) *Gate {
	name := "unknown"
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return &Gate{reg: reg, audit: log, user: name, changes: map[string]*Change{}}
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "chg_" + hex.EncodeToString(b)
}

// Propose computes the diff of a plan by dry-run and records it. Nothing is
// written to the cluster.
func (g *Gate) Propose(ctx context.Context, clusterName string, author Author, intent string, plan []Op) (*Change, error) {
	sess, err := g.reg.Session(clusterName)
	if err != nil {
		return nil, err
	}
	if len(plan) == 0 {
		return nil, errors.New("empty plan")
	}
	if author.Name == "" {
		author.Name = g.user
	}
	blast, err := blastOf(plan)
	if err != nil {
		return nil, err
	}
	var diffs, reviewed []string
	for i := range plan {
		d, was, err := g.dryRun(ctx, sess, plan[i])
		if err != nil {
			return nil, fmt.Errorf("%s %s/%s: %w", plan[i].Op, plan[i].Kind, plan[i].Name, err)
		}
		diffs = append(diffs, d)
		reviewed = append(reviewed, was)
	}
	c := &Change{
		ID: newID(), Cluster: clusterName, Author: author, Intent: intent, Plan: plan,
		Diff: strings.Join(diffs, "\n"), Blast: blast, Status: "pending", CreatedAt: time.Now().UTC(),
		reviewed: reviewed,
	}
	g.mu.Lock()
	g.changes[c.ID] = c
	g.mu.Unlock()
	if err := g.record("change.proposed", c, author.Name); err != nil {
		return nil, fmt.Errorf("refusing to hold an unrecorded change: %w", err)
	}
	return c, nil
}

// Decide approves or rejects. Approval applies immediately and records the
// outcome. Only a human may decide.
func (g *Gate) Decide(ctx context.Context, id, decision, note string) (*Change, error) {
	// The check and the transition happen under one hold of the lock. Two
	// decisions arriving together on the same change would otherwise both
	// read "pending" and both apply the plan, which for a delete or a
	// restart means doing it twice.
	g.mu.Lock()
	c, ok := g.changes[id]
	if !ok {
		g.mu.Unlock()
		return nil, fmt.Errorf("unknown change %s", id)
	}
	if c.Status != "pending" {
		status := c.Status
		g.mu.Unlock()
		return nil, fmt.Errorf("change %s is %s", id, status)
	}
	c.DecidedAt, c.DecidedBy, c.Note = time.Now().UTC(), g.user, note
	if decision != "approve" {
		c.Status = "rejected"
		g.mu.Unlock()
		if err := g.record("change.rejected", c, g.user); err != nil {
			return nil, err
		}
		return g.copyOf(id), nil
	}
	c.Status = "approved"
	g.mu.Unlock()

	// Nothing is applied until the approval is on disk. A change that
	// applies without a record is exactly what the gate exists to prevent.
	if err := g.record("change.approved", c, g.user); err != nil {
		g.setFailed(id, fmt.Sprintf("the approval could not be recorded, so nothing was applied: %v", err))
		_ = g.record("change.failed", c, g.user)
		return g.copyOf(id), nil
	}

	sess, err := g.reg.Session(c.Cluster)
	if err != nil {
		g.setFailed(id, err.Error())
		_ = g.record("change.failed", c, g.user)
		return g.copyOf(id), nil
	}
	for i, op := range c.Plan {
		was := ""
		if i < len(c.reviewed) {
			was = c.reviewed[i]
		}
		if err := g.execute(ctx, sess, op, was); err != nil {
			g.setFailed(id, fmt.Sprintf("%s %s/%s: %v", op.Op, op.Kind, op.Name, err))
			_ = g.record("change.failed", c, g.user)
			return g.copyOf(id), nil
		}
	}
	g.mu.Lock()
	c.Status = "applied"
	g.mu.Unlock()
	if err := g.record("change.applied", c, g.user); err != nil {
		return g.copyOf(id), fmt.Errorf("the change applied but could not be recorded: %w", err)
	}
	return g.copyOf(id), nil
}

func (g *Gate) setFailed(id, msg string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if c, ok := g.changes[id]; ok {
		c.Status, c.Error = "failed", msg
	}
}

// copyOf returns a snapshot, so a caller reading the change cannot race a
// later status write.
func (g *Gate) copyOf(id string) *Change {
	g.mu.Lock()
	defer g.mu.Unlock()
	c, ok := g.changes[id]
	if !ok {
		return nil
	}
	cp := *c
	return &cp
}

// List returns changes newest first.
func (g *Gate) List() []Change {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Change, 0, len(g.changes))
	for _, c := range g.changes {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (g *Gate) record(action string, c *Change, actor string) error {
	detail, err := json.Marshal(map[string]any{"intent": c.Intent, "plan": c.Plan, "blast": c.Blast, "status": c.Status, "error": c.Error, "author": c.Author})
	if err != nil {
		return fmt.Errorf("audit detail cannot be encoded: %w", err)
	}
	_, err = g.audit.Append(audit.Entry{Actor: actor, Action: action, ChangeID: c.ID, Cluster: c.Cluster, Detail: detail})
	return err
}

// blastOf describes what a plan reaches. It resolves every op through the
// kind registry first, because the raw strings and the object the apply
// actually touches can disagree: "Secret" and "secrets" name the same kind,
// and a namespace on a cluster-scoped kind is discarded at write time. Both
// of those differences were visible to the reviewer as a smaller blast
// radius than the truth.
func blastOf(plan []Op) (Blast, error) {
	ns, kindNames := map[string]bool{}, map[string]bool{}
	b := Blast{Objects: len(plan)}
	for _, op := range plan {
		k, err := kinds.Lookup(op.Kind)
		if err != nil {
			return Blast{}, fmt.Errorf("%s %s/%s: %w", op.Op, op.Kind, op.Name, err)
		}
		if k.Namespaced && op.Namespace != "" {
			ns[op.Namespace] = true
		}
		kindNames[k.Kind] = true
		if k.GVR.Resource == "secrets" {
			b.HasSecrets = true
		}
		if op.Op == "delete" {
			b.Deletes++
		}
	}
	for k := range ns {
		b.Namespaces = append(b.Namespaces, k)
	}
	for k := range kindNames {
		b.Kinds = append(b.Kinds, k)
	}
	sort.Strings(b.Namespaces)
	sort.Strings(b.Kinds)
	return b, nil
}

// desired builds the object an op would produce, from the live object.
func desired(op Op, live *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	switch op.Op {
	case "apply":
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(op.YAML), &u.Object); err != nil {
			return nil, fmt.Errorf("parse yaml: %w", err)
		}
		// The API server takes the name from the body, not from the request
		// path, so a body naming something else would be written under a
		// name the reviewer never saw. Refuse rather than surprise them.
		if gn, _, _ := unstructured.NestedString(u.Object, "metadata", "generateName"); gn != "" {
			return nil, errors.New("metadata.generateName is not allowed: a change must name the object it writes")
		}
		if n := u.GetName(); n != op.Name {
			return nil, fmt.Errorf("the yaml names %q but this change is for %q", n, op.Name)
		}
		if n := u.GetNamespace(); n != "" && n != op.Namespace {
			return nil, fmt.Errorf("the yaml is in namespace %q but this change is for %q", n, op.Namespace)
		}
		if live != nil && u.GetResourceVersion() == "" {
			u.SetResourceVersion(live.GetResourceVersion())
		}
		return u, nil
	case "scale":
		if live == nil {
			return nil, errors.New("object not found")
		}
		if op.Replicas == nil {
			return nil, errors.New("replicas required")
		}
		u := live.DeepCopy()
		_ = unstructured.SetNestedField(u.Object, int64(*op.Replicas), "spec", "replicas")
		return u, nil
	case "restart":
		if live == nil {
			return nil, errors.New("object not found")
		}
		u := live.DeepCopy()
		_ = unstructured.SetNestedField(u.Object, time.Now().UTC().Format(time.RFC3339), "spec", "template", "metadata", "annotations", "kubectl.kubernetes.io/restartedAt")
		return u, nil
	case "delete":
		return nil, nil
	}
	return nil, fmt.Errorf("unknown op %q", op.Op)
}

// dryRun renders the diff and returns the live object as the diff renders
// it. That rendering is what the human is shown, so it is what the apply
// checks against before writing.
func (g *Gate) dryRun(ctx context.Context, sess *cluster.Session, op Op) (string, string, error) {
	k, err := kinds.Lookup(op.Kind)
	if err != nil {
		return "", "", err
	}
	if op.Op == "scale" && !k.Scalable {
		return "", "", fmt.Errorf("%s cannot be scaled", k.Kind)
	}
	if op.Op == "restart" && !k.Restarts {
		return "", "", fmt.Errorf("%s cannot be restarted", k.Kind)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res := sess.Dynamic.Resource(k.GVR).Namespace(nsFor(k, op.Namespace))

	live, err := res.Get(ctx, op.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		live = nil
	} else if err != nil {
		return "", "", err
	}
	before := ""
	if live != nil {
		before = watch.ToDiffYAML(live)
	}
	header := fmt.Sprintf("%s %s/%s", op.Op, k.Kind, op.Name)

	if op.Op == "delete" {
		if live == nil {
			return "", "", errors.New("object not found")
		}
		return unified(header, before, ""), before, nil
	}
	want, err := desired(op, live)
	if err != nil {
		return "", "", err
	}
	// A dry run asks what the object would look like, not to write it
	// atomically. Sending the resourceVersion makes the API server treat it
	// as a precondition, so taking a diff of a Deployment whose pods are
	// still settling fails with a conflict, for a request that was never
	// going to store anything.
	want.SetResourceVersion("")
	dry := metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}, FieldManager: FieldManager}
	var after *unstructured.Unstructured
	if live == nil {
		after, err = res.Create(ctx, want, metav1.CreateOptions{DryRun: dry.DryRun, FieldManager: FieldManager})
	} else {
		after, err = res.Update(ctx, want, dry)
	}
	if err != nil {
		return "", "", err
	}
	return unified(header, before, watch.ToDiffYAML(after)), before, nil
}

// execute applies one op, after checking that the object is still the one
// the reviewer saw.
func (g *Gate) execute(ctx context.Context, sess *cluster.Session, op Op, reviewed string) error {
	k, err := kinds.Lookup(op.Kind)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res := sess.Dynamic.Resource(k.GVR).Namespace(nsFor(k, op.Namespace))

	live, err := res.Get(ctx, op.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		live = nil
	} else if err != nil {
		return err
	}
	now := ""
	if live != nil {
		now = watch.ToDiffYAML(live)
	}
	if now != reviewed {
		switch {
		case reviewed == "":
			return fmt.Errorf("%s now exists; it did not when this change was reviewed, so approving it would overwrite something nobody looked at", op.Name)
		case now == "":
			return fmt.Errorf("%s no longer exists; it did when this change was reviewed", op.Name)
		default:
			return fmt.Errorf("%s changed after this change was reviewed, so the diff you approved is out of date; propose it again against what is there now", op.Name)
		}
	}

	switch op.Op {
	case "delete":
		return res.Delete(ctx, op.Name, metav1.DeleteOptions{})
	case "restart":
		patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
			time.Now().UTC().Format(time.RFC3339))
		_, err := res.Patch(ctx, op.Name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{FieldManager: FieldManager})
		return err
	case "scale":
		patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, *op.Replicas)
		_, err := res.Patch(ctx, op.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{FieldManager: FieldManager})
		return err
	case "apply":
		want, err := desired(op, live)
		if err != nil {
			return err
		}
		if live == nil {
			_, err = res.Create(ctx, want, metav1.CreateOptions{FieldManager: FieldManager})
			return err
		}
		// The object matched what was reviewed a moment ago, so carrying the
		// version from that same read closes the remaining gap: anything
		// that lands in between makes this a conflict rather than an
		// overwrite of work nobody looked at.
		want.SetResourceVersion(live.GetResourceVersion())
		_, err = res.Update(ctx, want, metav1.UpdateOptions{FieldManager: FieldManager})
		return err
	}
	return fmt.Errorf("unknown op %q", op.Op)
}

func nsFor(k kinds.Kind, ns string) string {
	if !k.Namespaced {
		return ""
	}
	return ns
}

func unified(header, before, after string) string {
	d := difflib.UnifiedDiff{A: difflib.SplitLines(before), B: difflib.SplitLines(after), FromFile: "live", ToFile: "desired", Context: 3}
	s, _ := difflib.GetUnifiedDiffString(d)
	if s == "" {
		s = "(no change)\n"
	}
	return "# " + header + "\n" + s
}

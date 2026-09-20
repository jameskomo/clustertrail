package watch

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
)

// Get fetches one object as YAML with managedFields removed, which is what a
// person wants to read and edit.
//
// This is the owner's own view of their own object, so it is not redacted.
// Anything leaving the machine must use GetRedacted instead.
func Get(ctx context.Context, sess *cluster.Session, kind kinds.Kind, namespace, name string) (string, error) {
	u, err := fetch(ctx, sess, kind, namespace, name)
	if err != nil {
		return "", err
	}
	return ToYAML(u), nil
}

// GetRedacted fetches one object as YAML with every Secret value replaced.
//
// Use this on any path that leaves this machine: the MCP tools a model reads
// through, and the explain pane, which posts to a third-party API. The owner
// can read their own Secrets in the interface; a model and a cloud provider
// have no business holding them.
func GetRedacted(ctx context.Context, sess *cluster.Session, kind kinds.Kind, namespace, name string) (string, error) {
	u, err := fetch(ctx, sess, kind, namespace, name)
	if err != nil {
		return "", err
	}
	return ToRedactedYAML(u), nil
}

func fetch(ctx context.Context, sess *cluster.Session, kind kinds.Kind, namespace, name string) (*unstructured.Unstructured, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return sess.Dynamic.Resource(kind.GVR).Namespace(nsFor(kind, namespace)).Get(ctx, name, metav1.GetOptions{})
}

// ToYAML renders an object for humans: no managedFields.
func ToYAML(u *unstructured.Unstructured) string {
	c := u.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	b, _ := yaml.Marshal(c.Object)
	return string(b)
}

// noisyAnnotations are written by the API server and by kubectl. They change
// on every write and say nothing about intent, so a diff that shows them is
// a diff nobody reads.
var noisyAnnotations = []string{
	"kubectl.kubernetes.io/last-applied-configuration",
	"deployment.kubernetes.io/revision",
}

// ToDiffYAML renders an object for diffing: server-managed counters and
// bookkeeping annotations are dropped so the diff shows only intent.
func ToDiffYAML(u *unstructured.Unstructured) string {
	c := u.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(c.Object, "metadata", "generation")
	unstructured.RemoveNestedField(c.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(c.Object, "metadata", "uid")
	unstructured.RemoveNestedField(c.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(c.Object, "status")
	for _, a := range noisyAnnotations {
		unstructured.RemoveNestedField(c.Object, "metadata", "annotations", a)
	}
	if ann, ok, _ := unstructured.NestedMap(c.Object, "metadata", "annotations"); ok && len(ann) == 0 {
		unstructured.RemoveNestedField(c.Object, "metadata", "annotations")
	}
	b, _ := yaml.Marshal(c.Object)
	return string(b)
}

// redactKey fingerprints Secret values so a redacted diff still detects a
// changed value. It is random per process, so a fingerprint means nothing
// outside the run that produced it and nothing can be recovered from it,
// including by dictionary attack on a short password.
var redactKey = func() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		// A process without a working CSPRNG has worse problems, but a
		// predictable key here must never become a readable secret.
		panic("clustertrail: no entropy for the redaction key: " + err.Error())
	}
	return k
}()

// fingerprint stands in for a Secret value that was deliberately not
// rendered. Equal values fingerprint alike within one process, so drift in a
// Secret is still visible; the value itself is not recoverable.
func fingerprint(v any) string {
	s, ok := v.(string)
	if !ok {
		return "<redacted>"
	}
	m := hmac.New(sha256.New, redactKey)
	m.Write([]byte(s))
	return "<redacted " + hex.EncodeToString(m.Sum(nil))[:8] + ">"
}

// ToRedactedYAML renders an object for somewhere other than this machine.
//
// Two things carry Secret values. The obvious one is a Secret's own data.
// The other is the last-applied-configuration annotation, which kubectl
// writes on every apply and which contains the complete object body, Secret
// data included. Stripping the first and leaving the second redacts nothing.
func ToRedactedYAML(u *unstructured.Unstructured) string {
	c := u.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	for _, a := range noisyAnnotations {
		unstructured.RemoveNestedField(c.Object, "metadata", "annotations", a)
	}
	if ann, ok, _ := unstructured.NestedMap(c.Object, "metadata", "annotations"); ok && len(ann) == 0 {
		unstructured.RemoveNestedField(c.Object, "metadata", "annotations")
	}
	Redact(c)
	b, _ := yaml.Marshal(c.Object)
	return string(b)
}

// Redact replaces the values of a Secret in place, keeping its keys so the
// shape stays readable. It reports the keys it redacted.
func Redact(u *unstructured.Unstructured) []string {
	if !strings.EqualFold(u.GetKind(), "Secret") {
		return nil
	}
	var redacted []string
	for _, field := range []string{"data", "stringData"} {
		raw, ok := u.Object[field]
		if !ok {
			continue
		}
		m, isMap := raw.(map[string]any)
		if !isMap {
			// Not the shape a Secret should have. Drop it rather than
			// letting an unexpected shape carry the values through.
			delete(u.Object, field)
			redacted = append(redacted, field)
			continue
		}
		for k, v := range m {
			m[k] = fingerprint(v)
			redacted = append(redacted, k)
		}
	}
	sort.Strings(redacted)
	return redacted
}

func nsFor(kind kinds.Kind, namespace string) string {
	if !kind.Namespaced {
		return ""
	}
	return namespace
}

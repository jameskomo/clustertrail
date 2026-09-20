package timeline

import (
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func fields(t *testing.T, raw string) *metav1.FieldsV1 {
	t.Helper()
	if !json.Valid([]byte(raw)) {
		t.Fatalf("test fixture is not valid JSON: %s", raw)
	}
	return &metav1.FieldsV1{Raw: []byte(raw)}
}

func TestFieldPathsReadsWhatAManagerWrote(t *testing.T) {
	// This is the shape Kubernetes stores in managedFields. Turning it into
	// "spec.replicas" is what makes the timeline readable.
	got := fieldPathsT(t, `{"f:spec":{"f:replicas":{}}}`)
	if len(got) != 1 || got[0] != "spec.replicas" {
		t.Fatalf("want [spec.replicas], got %v", got)
	}
}

func fieldPathsT(t *testing.T, raw string) []string { return fieldPaths(fields(t, raw)) }

func TestFieldPathsStopsDescendingAtDepthTwo(t *testing.T) {
	got := fieldPathsT(t, `{"f:spec":{"f:template":{"f:spec":{"f:containers":{"k:{\"name\":\"web\"}":{"f:image":{}}}}}}}`)
	if len(got) != 1 {
		t.Fatalf("want one path, got %v", got)
	}
	if !strings.HasPrefix(got[0], "spec.template") {
		t.Errorf("want a path rooted at spec.template, got %q", got[0])
	}
	if strings.Count(got[0], ".") > 2 {
		t.Errorf("path should stay shallow enough to read, got %q", got[0])
	}
}

func TestFieldPathsIgnoresMetadataAndStatus(t *testing.T) {
	// Every write touches metadata, and status is reconciliation noise.
	// Neither says anything about intent.
	got := fieldPathsT(t, `{"f:metadata":{"f:annotations":{}},"f:status":{"f:replicas":{}},"f:spec":{"f:replicas":{}}}`)
	if len(got) != 1 || got[0] != "spec.replicas" {
		t.Fatalf("want only spec.replicas, got %v", got)
	}
}

func TestFieldPathsCapsALongList(t *testing.T) {
	got := fieldPathsT(t, `{"f:spec":{"f:a":{},"f:b":{},"f:c":{},"f:d":{},"f:e":{},"f:f":{}}}`)
	if len(got) != 5 {
		t.Fatalf("want 4 paths plus a summary, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[4], "more") {
		t.Errorf("the last entry should say how many were elided, got %q", got[4])
	}
}

func TestFieldPathsHandlesNothingToSay(t *testing.T) {
	if got := fieldPaths(nil); got != nil {
		t.Errorf("no fields means no paths, got %v", got)
	}
	if got := fieldPathsT(t, `{}`); len(got) != 0 {
		t.Errorf("an empty tree means no paths, got %v", got)
	}
	if got := fieldPaths(&metav1.FieldsV1{Raw: []byte("not json")}); got != nil {
		t.Errorf("unparseable fields must be skipped, not panic, got %v", got)
	}
}

func TestImageDeltaDescribesOnlyRealChanges(t *testing.T) {
	if got := imageDelta([]string{"nginx:1.27"}, []string{"nginx:1.27"}); got != "" {
		t.Errorf("an unchanged image is not a delta, got %q", got)
	}
	got := imageDelta([]string{"nginx:1.27"}, []string{"nginx:1.28"})
	if !strings.Contains(got, "1.27") || !strings.Contains(got, "1.28") {
		t.Errorf("the delta must name both sides, got %q", got)
	}
}

func TestInterestingKeepsCausesAndSymptoms(t *testing.T) {
	warning := &corev1.Event{Type: corev1.EventTypeWarning, Reason: "BackOff"}
	if !interesting(warning) {
		t.Error("every warning is worth a line")
	}
	scaling := &corev1.Event{Type: corev1.EventTypeNormal, Reason: "ScalingReplicaSet"}
	if !interesting(scaling) {
		t.Error("a scaling decision is a cause and must be kept")
	}
}

func TestInterestingDropsConsequenceNoise(t *testing.T) {
	// A rollout already has its own line. The kubelet events it produces add
	// length without adding information.
	for _, reason := range []string{"Created", "Started", "Pulled", "SuccessfulCreate", "Scheduled"} {
		e := &corev1.Event{Type: corev1.EventTypeNormal, Reason: reason}
		if interesting(e) {
			t.Errorf("%s is a consequence of a change already shown, and should be dropped", reason)
		}
	}
}

func TestControllerWritesAreMarkedAsMachine(t *testing.T) {
	if !controllerManagers["kube-controller-manager"] {
		t.Error("controller writes must be distinguishable from a person's")
	}
	if controllerManagers["kubectl-edit"] {
		t.Error("a person's kubectl write must not be filed as machine")
	}
	if machineNote(true) == "" {
		t.Error("a machine write should say so, so a reviewer can discount it")
	}
	if machineNote(false) != "" {
		t.Error("a human write needs no note")
	}
}

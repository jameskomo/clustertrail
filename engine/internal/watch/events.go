package watch

import (
	"context"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/rows"
)

// EventsFor lists the events of one object, newest first.
func EventsFor(ctx context.Context, sess *cluster.Session, namespace, kind, name string) ([]rows.EventRow, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	sel := fields.Set{"involvedObject.name": name, "involvedObject.namespace": namespace}
	if kind != "" {
		sel["involvedObject.kind"] = kind
	}
	list, err := sess.Client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: sel.AsSelector().String()})
	if err != nil {
		return nil, err
	}
	out := make([]rows.EventRow, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, rows.ProjectEvent(&list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

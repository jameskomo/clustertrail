// Package watch turns informer events into batched row deltas for one
// subscription. Upserts and deletes are idempotent by key, so the UI can apply
// them in any order relative to the snapshot.
package watch

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
	"github.com/jameskomo/clustertrail/engine/internal/rows"
)

// FlushInterval bounds how often a subscription emits a delta. Ten frames a
// second is plenty for a table and keeps 10k-object churn cheap.
const FlushInterval = 100 * time.Millisecond

// Sink receives the snapshot and subsequent deltas of a subscription.
type Sink interface {
	Snapshot(rows []rows.Row, metricsAvailable bool)
	Delta(upsert []rows.Row, del []string)
	Error(err error)
}

type batcher struct {
	mu      sync.Mutex
	upserts map[string]rows.Row
	deletes map[string]struct{}
}

func newBatcher() *batcher {
	return &batcher{upserts: map[string]rows.Row{}, deletes: map[string]struct{}{}}
}

func (b *batcher) upsert(r rows.Row) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.deletes, r.Key)
	b.upserts[r.Key] = r
}

func (b *batcher) del(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.upserts, key)
	b.deletes[key] = struct{}{}
}

func (b *batcher) drain() ([]rows.Row, []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.upserts) == 0 && len(b.deletes) == 0 {
		return nil, nil
	}
	up := make([]rows.Row, 0, len(b.upserts))
	for _, r := range b.upserts {
		up = append(up, r)
	}
	del := make([]string, 0, len(b.deletes))
	for k := range b.deletes {
		del = append(del, k)
	}
	b.upserts = map[string]rows.Row{}
	b.deletes = map[string]struct{}{}
	return up, del
}

func keyOf(u *unstructured.Unstructured) string { return rows.Key(u.GetNamespace(), u.GetName()) }

// Resources subscribes to one kind in a namespace ("" = all) and streams rows
// to sink until ctx is cancelled. Returns immediately; the snapshot arrives
// once the informer has synced. Pods and nodes are decorated with metrics
// and re-emitted whenever the metrics cache refreshes.
func Resources(ctx context.Context, sess *cluster.Session, kind kinds.Kind, namespace string, sink Sink) {
	if !kind.Namespaced {
		namespace = ""
	}
	f, release := sess.Factory(namespace)
	inf := f.ForResource(kind.GVR).Informer()
	lister := f.ForResource(kind.GVR).Lister()
	b := newBatcher()

	project := func(obj any) (rows.Row, bool) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return rows.Row{}, false
		}
		r, err := kind.Project(u)
		if err != nil {
			return rows.Row{}, false
		}
		sess.Metrics.Decorate(kind.Key, &r)
		return r, true
	}

	reg, err := inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if r, ok := project(obj); ok {
				b.upsert(r)
			}
		},
		UpdateFunc: func(_, obj any) {
			if r, ok := project(obj); ok {
				b.upsert(r)
			}
		},
		DeleteFunc: func(obj any) {
			if t, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = t.Obj
			}
			if u, ok := obj.(*unstructured.Unstructured); ok {
				b.del(keyOf(u))
			}
		},
	})
	if err != nil {
		release()
		sink.Error(err)
		return
	}
	// Surface list/watch failures (RBAC, auth) instead of syncing forever.
	_ = inf.SetWatchErrorHandler(func(_ *cache.Reflector, err error) { sink.Error(err) })
	sess.Start(f)

	wantMetrics := kind.Key == "pods" || kind.Key == "nodes"
	var metricsTick <-chan struct{}
	if wantMetrics {
		ch, cancel := sess.Metrics.Subscribe()
		metricsTick = ch
		defer func() { go func() { <-ctx.Done(); cancel() }() }()
	}

	snapshot := func() []rows.Row {
		objs, _ := lister.List(labels.Everything())
		out := make([]rows.Row, 0, len(objs))
		for _, o := range objs {
			if r, ok := project(o); ok {
				out = append(out, r)
			}
		}
		return out
	}

	// lastMetrics is the cpu/mem last sent for each row, so a metrics poll
	// can emit what actually moved.
	lastMetrics := map[string][2]int64{}
	metricsOf := func(r rows.Row) [2]int64 {
		var out [2]int64
		if v, ok := r.Cells["cpu"].(int64); ok {
			out[0] = v
		}
		if v, ok := r.Cells["mem"].(int64); ok {
			out[1] = v
		}
		return out
	}

	go func() {
		defer release()
		defer func() { _ = inf.RemoveEventHandler(reg) }()
		if !cache.WaitForCacheSync(ctx.Done(), inf.HasSynced) {
			return
		}
		sink.Snapshot(snapshot(), sess.Metrics.Available())

		t := time.NewTicker(FlushInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-metricsTick:
				// Only the rows whose usage moved, fed through the same
				// batcher as everything else. Re-sending the whole table
				// every fifteen seconds meant an idle user watching an idle
				// cluster still paid for a full re-projection, a full
				// re-encode and a multi-megabyte frame, per subscription,
				// forever.
				for _, r := range snapshot() {
					m := metricsOf(r)
					if last, seen := lastMetrics[r.Key]; seen && last == m {
						continue
					}
					lastMetrics[r.Key] = m
					b.upsert(r)
				}
			case <-t.C:
				if up, del := b.drain(); up != nil || del != nil {
					sink.Delta(up, del)
				}
			}
		}
	}()
}

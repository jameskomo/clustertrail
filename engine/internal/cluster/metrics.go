package cluster

import (
	"context"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/jameskomo/clustertrail/engine/internal/rows"
)

// MetricsCache polls metrics.k8s.io on demand and remembers the last answer.
// Polling starts with the first subscriber and stops when the session closes.
// If metrics-server is absent, Available is false and rows show no bars.
type MetricsCache struct {
	client metricsclient.Interface
	stop   <-chan struct{}

	mu        sync.RWMutex
	pods      map[string]rows.PodMetrics
	nodes     map[string]rows.NodeMetrics
	history   map[string][]rows.Sample // key → ring of samples, newest last
	available bool
	started   bool
	listeners map[chan struct{}]struct{}
}

const pollEvery = 15 * time.Second

// HistoryLen is how many samples are kept per object: 120 × 15 s = 30 minutes.
const HistoryLen = 120

// maxTrackedKeys bounds how many objects keep a history ring.
//
// HistoryLen caps each ring, but the number of rings was the cluster's pod
// count, because the poll lists every pod in every namespace. At roughly 6 KB
// retained per pod that is about 600 MB on a 100,000-pod cluster, in a
// desktop tool somebody opened to look at one namespace.
const maxTrackedKeys = 5000

func (m *MetricsCache) record(key string, cpu, mem int64, at time.Time) {
	h, known := m.history[key]
	if !known && len(m.history) >= maxTrackedKeys {
		// Already tracking as much as this is willing to hold. Existing
		// rings keep updating; a new object simply has no chart history,
		// which is a better outcome than exhausting the machine.
		return
	}
	h = append(h, rows.Sample{At: at, CPUMilli: cpu, MemBytes: mem})
	if len(h) > HistoryLen {
		h = h[len(h)-HistoryLen:]
	}
	m.history[key] = h
}

func newMetricsCache(c metricsclient.Interface, stop <-chan struct{}) *MetricsCache {
	return &MetricsCache{client: c, stop: stop, pods: map[string]rows.PodMetrics{}, nodes: map[string]rows.NodeMetrics{}, history: map[string][]rows.Sample{}, listeners: map[chan struct{}]struct{}{}}
}

// EnsureStarted begins polling without registering a listener.
//
// Callers that only want the poll running must use this. Calling Subscribe
// and dropping the cancel leaves an entry in the listeners map forever, and
// the chart panes ask for history every fifteen seconds.
func (m *MetricsCache) EnsureStarted() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		m.started = true
		go m.loop()
	}
}

// Subscribe starts polling if needed and returns a channel that ticks after
// each refresh. Call the returned cancel when done.
func (m *MetricsCache) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.listeners[ch] = struct{}{}
	if !m.started {
		m.started = true
		go m.loop()
	}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.listeners, ch)
		m.mu.Unlock()
	}
}

func (m *MetricsCache) loop() {
	m.refresh()
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.refresh()
		}
	}
}

func (m *MetricsCache) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pm, err := m.client.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{})
	now := time.Now()
	m.mu.Lock()
	if err != nil {
		m.available = false
	} else {
		m.available = true
		m.pods = make(map[string]rows.PodMetrics, len(pm.Items))
		seen := map[string]bool{}
		var totalCPU, totalMem int64
		for _, p := range pm.Items {
			var cpu, mem int64
			for _, c := range p.Containers {
				cpu += c.Usage.Cpu().MilliValue()
				mem += c.Usage.Memory().Value()
			}
			key := rows.Key(p.Namespace, p.Name)
			m.pods[key] = rows.PodMetrics{CPUMilli: cpu, MemBytes: mem}
			m.record("pod/"+key, cpu, mem, now)
			seen["pod/"+key] = true
			totalCPU += cpu
			totalMem += mem
		}
		m.record("cluster", totalCPU, totalMem, now)
		for k := range m.history {
			if len(k) > 4 && k[:4] == "pod/" && !seen[k] {
				delete(m.history, k) // pod is gone; free its ring
			}
		}
	}
	m.mu.Unlock()
	if nm, err := m.client.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{}); err == nil {
		m.mu.Lock()
		m.nodes = make(map[string]rows.NodeMetrics, len(nm.Items))
		for _, n := range nm.Items {
			cpu, mem := n.Usage.Cpu().MilliValue(), n.Usage.Memory().Value()
			m.nodes[n.Name] = rows.NodeMetrics{CPUMilli: cpu, MemBytes: mem}
			m.record("node/"+n.Name, cpu, mem, now)
		}
		m.mu.Unlock()
	}
	m.mu.RLock()
	for ch := range m.listeners {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	m.mu.RUnlock()
}

func (m *MetricsCache) Available() bool { m.mu.RLock(); defer m.mu.RUnlock(); return m.available }

// Decorate adds cpu/mem cells to a pod or node row if metrics are known.
func (m *MetricsCache) Decorate(kind string, r *rows.Row) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	switch kind {
	case "pods":
		if pm, ok := m.pods[r.Key]; ok {
			r.Cells["cpu"] = pm.CPUMilli
			r.Cells["mem"] = pm.MemBytes
		}
	case "nodes":
		if nm, ok := m.nodes[r.Key]; ok {
			r.Cells["cpu"] = nm.CPUMilli
			r.Cells["mem"] = nm.MemBytes
		}
	}
}

// History returns the samples for "pod/<ns>/<name>", "node/<name>" or
// "cluster" (the sum over all pods), oldest first. Pods and nodes not yet
// sampled return an empty slice, never nil.
func (m *MetricsCache) History(key string) []rows.Sample {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h := m.history[key]
	out := make([]rows.Sample, len(h))
	copy(out, h)
	return out
}

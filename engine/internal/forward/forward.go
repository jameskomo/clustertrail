// Package forward owns port-forwards. They belong to the engine, not the UI,
// so a UI reload does not drop them; they die with the engine.
package forward

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
)

type Forward struct {
	ID        string `json:"id"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Port      int    `json:"port"`
	LocalPort int    `json:"localPort"`
	Error     string `json:"error,omitempty"`
	stop      chan struct{}
}

type Manager struct {
	mu   sync.Mutex
	list map[string]*Forward
	n    int
}

func NewManager() *Manager { return &Manager{list: map[string]*Forward{}} }

// Start forwards localPort (0 = pick free) to pod:port and returns once the
// listener is ready or the attempt failed.
func (m *Manager) Start(ctx context.Context, sess *cluster.Session, namespace, pod string, port, localPort int) (*Forward, error) {
	req := sess.Client.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).SubResource("portforward")
	transport, upgrader, err := spdy.RoundTripperFor(sess.Rest)
	if err != nil {
		return nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())
	stop, ready := make(chan struct{}), make(chan struct{})
	errOut := &sinkWriter{}
	pf, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("%d:%d", localPort, port)}, stop, ready, &sinkWriter{}, errOut)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.n++
	f := &Forward{ID: fmt.Sprintf("pf_%d", m.n), Cluster: sess.Name, Namespace: namespace, Pod: pod, Port: port, stop: stop}
	m.mu.Unlock()

	errc := make(chan error, 1)
	go func() { errc <- pf.ForwardPorts() }()
	select {
	case err := <-errc:
		if err == nil {
			err = fmt.Errorf("port-forward ended: %s", errOut.String())
		}
		return nil, err
	case <-ready:
	case <-ctx.Done():
		close(stop)
		return nil, ctx.Err()
	}
	ports, err := pf.GetPorts()
	if err != nil {
		close(stop)
		return nil, err
	}
	f.LocalPort = int(ports[0].Local)
	m.mu.Lock()
	m.list[f.ID] = f
	m.mu.Unlock()
	go func() {
		if err := <-errc; err != nil {
			m.mu.Lock()
			f.Error = err.Error()
			m.mu.Unlock()
		}
	}()
	return f, nil
}

func (m *Manager) Stop(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.list[id]; ok {
		close(f.stop)
		delete(m.list, id)
	}
}

func (m *Manager) List() []Forward {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Forward, 0, len(m.list))
	for _, f := range m.list {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

type sinkWriter struct {
	mu sync.Mutex
	b  []byte
}

func (w *sinkWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.b) < 4096 {
		w.b = append(w.b, p...)
	}
	return len(p), nil
}

func (w *sinkWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.b)
}

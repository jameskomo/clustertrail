// Package cluster loads the kubeconfig and hands out lazily connected sessions.
// Nothing connects until a screen asks for it, so listing contexts never
// triggers an auth plugin.
package cluster

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/jameskomo/clustertrail/engine/internal/rows"
)

// Info is what the cluster list shows before any connection is made.
type Info struct {
	Name    string `json:"name"`
	Server  string `json:"server"`
	Current bool   `json:"current"`
}

// Session is one connected context: clients and informer factories keyed by
// namespace ("" = cluster-wide), plus a metrics cache.
type Session struct {
	Name    string
	Rest    *rest.Config
	Client  kubernetes.Interface
	Dynamic dynamic.Interface
	Metrics *MetricsCache

	mu        sync.Mutex
	factories map[string]*factoryEntry
	stop      chan struct{}
}

// factoryEntry is a factory and the number of subscriptions holding it.
type factoryEntry struct {
	f    dynamicinformer.DynamicSharedInformerFactory
	stop chan struct{}
	refs int
}

// Factory returns the dynamic informer factory for a namespace and a release
// func to call when the caller is finished with it.
//
// The release matters. An informer nobody is watching is not idle: it keeps
// every object of its kind in memory, a reflector goroutine, and an open
// watch against the API server. Without this, closing a table or switching
// namespace freed nothing, and browsing N namespaces across M kinds left
// N times M of them running until the process exited.
func (s *Session) Factory(namespace string) (dynamicinformer.DynamicSharedInformerFactory, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.factories[namespace]
	if !ok {
		e = &factoryEntry{
			f:    dynamicinformer.NewFilteredDynamicSharedInformerFactory(s.Dynamic, 0, namespace, nil),
			stop: make(chan struct{}),
		}
		s.factories[namespace] = e
	}
	e.refs++
	var once sync.Once
	return e.f, func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			e.refs--
			if e.refs > 0 {
				return
			}
			close(e.stop)
			if s.factories[namespace] == e {
				delete(s.factories, namespace)
			}
		})
	}
}

// Start runs any informers registered on f since the last call. It stops
// them when the last holder of f releases it.
func (s *Session) Start(f dynamicinformer.DynamicSharedInformerFactory) {
	s.mu.Lock()
	stop := s.stop
	for _, e := range s.factories {
		if e.f == f {
			stop = e.stop
			break
		}
	}
	s.mu.Unlock()
	f.Start(stop)
}

// Registry is the set of contexts in the kubeconfig and their sessions.
// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// readOnlyTransport refuses any request to the API server that is not a read
// or an explicit server-side dry run.
//
// This is the difference between "the tools do not write" and "the session
// cannot write". The first is a property of the code as it stands today and
// has to be re-established every time someone adds a tool; the second holds
// whatever the tools do.
func readOnlyTransport(rt http.RoundTripper) http.RoundTripper {
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return rt.RoundTrip(req)
		}
		// A dry run is how an honest diff is taken: the API server runs the
		// whole admission chain and returns what it would have stored,
		// without storing it. Drift depends on that, so it is allowed, and
		// it is the only non-read that is.
		if req.URL.Query().Get("dryRun") == "All" {
			return rt.RoundTrip(req)
		}
		return nil, fmt.Errorf("clustertrail: refusing %s %s, this session is read-only", req.Method, req.URL.Path)
	})
}

type Registry struct {
	rules  *clientcmd.ClientConfigLoadingRules
	config clientcmdapi.Config

	mu       sync.Mutex
	sessions map[string]*Session

	// ReadOnly makes every session this registry hands out refuse anything
	// but reads and dry runs, at the transport. The MCP server sets it.
	ReadOnly bool
}

// Load reads the kubeconfig using kubectl's rules: an explicit path, else
// $KUBECONFIG (a list), else ~/.kube/config.
func Load(explicitPath string) (*Registry, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicitPath != "" {
		rules.ExplicitPath = explicitPath
	}
	cfg, err := rules.Load()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return &Registry{rules: rules, config: *cfg, sessions: map[string]*Session{}}, nil
}

// Clusters lists every context, sorted, without connecting.
func (r *Registry) Clusters() []Info {
	out := make([]Info, 0, len(r.config.Contexts))
	for name, ctx := range r.config.Contexts {
		server := ""
		if c, ok := r.config.Clusters[ctx.Cluster]; ok {
			server = c.Server
		}
		out = append(out, Info{Name: name, Server: server, Current: name == r.config.CurrentContext})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Session returns the connected session for a context, connecting on first use.
func (r *Registry) Session(name string) (*Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[name]; ok {
		return s, nil
	}
	if _, ok := r.config.Contexts[name]; !ok {
		return nil, fmt.Errorf("unknown context %q", name)
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(r.config, name, &clientcmd.ConfigOverrides{}, r.rules).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", name, err)
	}
	// A desktop client opens many watches; the defaults are tuned for controllers.
	cfg.QPS = 50
	cfg.Burst = 100
	cfg.UserAgent = "clustertrail"
	if r.ReadOnly {
		cfg.Wrap(readOnlyTransport)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", name, err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", name, err)
	}
	mc, err := metricsclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", name, err)
	}
	s := &Session{Name: name, Rest: cfg, Client: client, Dynamic: dyn, factories: map[string]*factoryEntry{}, stop: make(chan struct{})}
	s.Metrics = newMetricsCache(mc, s.stop)
	r.sessions[name] = s
	return s, nil
}

// Close stops every informer in every session.
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		close(s.stop)
	}
}

// Ctx returns a context that ends when the session closes.
func (s *Session) Ctx(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-s.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

var _ = time.Second
var _ rows.Row

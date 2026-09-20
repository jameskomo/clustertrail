package api

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jameskomo/clustertrail/engine/internal/audit"
	"github.com/jameskomo/clustertrail/engine/internal/change"
	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/drift"
	"github.com/jameskomo/clustertrail/engine/internal/explain"
	"github.com/jameskomo/clustertrail/engine/internal/forward"
	"github.com/jameskomo/clustertrail/engine/internal/helm"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
	"github.com/jameskomo/clustertrail/engine/internal/rows"
	"github.com/jameskomo/clustertrail/engine/internal/store"
	"github.com/jameskomo/clustertrail/engine/internal/term"
	"github.com/jameskomo/clustertrail/engine/internal/timeline"
	"github.com/jameskomo/clustertrail/engine/internal/ui"
	"github.com/jameskomo/clustertrail/engine/internal/watch"
)

// Server serves the local protocol. It is bound to loopback by main and
// requires a per-launch token so other local processes cannot borrow the
// user's cluster credentials.
type Server struct {
	Registry  *cluster.Registry
	Gate      *change.Gate
	Audit     *audit.Log
	Forwards  *forward.Manager
	Snapshots *store.Store
	Token     string
	// Addr is the address the listener actually bound, used to reject a
	// request that arrived under some other name.
	Addr    string
	Version string
	Log     *slog.Logger
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /ws", s.serveWS)
	// The interface is public; the socket behind it is not. Serving the page
	// without a token is deliberate: the HTML is not a secret, and demanding
	// one here would only mean pasting it into a browser by hand.
	mux.Handle("/", ui.Handler())
	return s.loopbackOnly(mux)
}

// loopbackOnly rejects any request that did not arrive addressed to this
// listener.
//
// Binding to 127.0.0.1 says nothing about DNS. A page on attacker.example
// whose record is flipped to 127.0.0.1 reaches this process, and because the
// browser then considers it same-origin, an Origin allowlist waves it
// through: under rebinding the Origin and the Host are both the attacker's
// name, which is exactly what "same origin" tests for. Checking the Host is
// what actually ties a request to the address the engine was reached on.
func (s *Server) loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostIsOurs(r.Host) {
			http.Error(w, "unexpected Host header", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostIsOurs(host string) bool {
	if s.Addr == "" {
		return true // not configured: the CLI always sets it
	}
	_, port, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return true
	}
	h, p, err := net.SplitHostPort(host)
	if err != nil {
		// A Host with no port cannot be this listener, which always has one.
		return false
	}
	if p != port {
		return false
	}
	switch strings.ToLower(h) {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return net.ParseIP(strings.Trim(h, "[]")).IsLoopback()
}

func (s *Server) authorized(r *http.Request) bool {
	tok := r.URL.Query().Get("token")
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimPrefix(h, "Bearer ")
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) == 1
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*", "tauri://localhost", "http://tauri.localhost"},
	})
	if err != nil {
		s.Log.Warn("ws accept", "err", err)
		return
	}
	conn := &connection{srv: s, ws: c, out: make(chan ServerMsg, 1024), subs: map[string]context.CancelFunc{}, terms: map[string]*term.Session{}}
	conn.run(r.Context())
}

// connection is one UI socket: a single writer goroutine, a reader loop, and
// the set of live subscriptions and terminals.
type connection struct {
	srv   *Server
	ws    *websocket.Conn
	out   chan ServerMsg
	mu    sync.Mutex
	subs  map[string]context.CancelFunc
	terms map[string]*term.Session
}

func (c *connection) send(m ServerMsg) {
	select {
	case c.out <- m:
	default:
		// A UI that cannot keep up gets disconnected rather than growing the
		// engine's memory; it reconnects and receives a fresh snapshot.
		c.srv.Log.Warn("ws client too slow, closing")
		_ = c.ws.Close(websocket.StatusPolicyViolation, "too slow")
	}
}

func (c *connection) fail(id, msg string) { c.send(ServerMsg{Type: "error", ID: id, Message: msg}) }

func (c *connection) run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	c.ws.SetReadLimit(4 << 20)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-c.out:
				wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
				err := wsjson.Write(wctx, c.ws, m)
				wcancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	c.send(ServerMsg{Type: "hello", Version: c.srv.Version})

	for {
		var m ClientMsg
		if err := wsjson.Read(ctx, c.ws, &m); err != nil {
			break
		}
		c.handle(ctx, m)
	}

	c.mu.Lock()
	for _, stop := range c.subs {
		stop()
	}
	for _, t := range c.terms {
		t.Close()
	}
	c.mu.Unlock()
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
}

func (c *connection) session(id, name string) (*cluster.Session, bool) {
	sess, err := c.srv.Registry.Session(name)
	if err != nil {
		c.fail(id, err.Error())
		return nil, false
	}
	return sess, true
}

// kind resolves a registry key, an API kind name, or "crd:<crd name>" for a
// custom resource, whose columns come from the CRD's printer columns.
func (c *connection) kind(ctx context.Context, id, cluster, key string) (kinds.Kind, bool) {
	if strings.HasPrefix(key, "crd:") {
		sess, ok := c.session(id, cluster)
		if !ok {
			return kinds.Kind{}, false
		}
		k, err := kinds.CustomFor(ctx, sess.Dynamic, strings.TrimPrefix(key, "crd:"))
		if err != nil {
			c.fail(id, err.Error())
			return kinds.Kind{}, false
		}
		return k, true
	}
	k, err := kinds.Lookup(key)
	if err != nil {
		c.fail(id, err.Error())
		return k, false
	}
	return k, true
}

func (c *connection) track(id string, stop context.CancelFunc) {
	c.mu.Lock()
	if old, ok := c.subs[id]; ok {
		old()
	}
	c.subs[id] = stop
	c.mu.Unlock()
}

func (c *connection) untrack(id string) {
	c.mu.Lock()
	if stop, ok := c.subs[id]; ok {
		stop()
		delete(c.subs, id)
	}
	c.mu.Unlock()
}

func (c *connection) handle(ctx context.Context, m ClientMsg) {
	switch m.Type {
	case "clusters":
		c.send(ServerMsg{Type: "clusters", Clusters: c.srv.Registry.Clusters()})

	case "subscribe":
		sess, ok := c.session(m.ID, m.Cluster)
		if !ok {
			return
		}
		subCtx, stop := context.WithCancel(ctx)
		c.track(m.ID, stop)
		if m.Kind == "logs" {
			watch.Logs(subCtx, sess, watch.LogOptions{Namespace: m.Namespace, Name: m.Name, Container: m.Container, Previous: m.Previous, Tail: m.Tail}, &sink{c: c, id: m.ID})
			return
		}
		k, ok := c.kind(ctx, m.ID, m.Cluster, m.Kind)
		if !ok {
			stop()
			return
		}
		// Paint whatever we saw last time first. The informer's snapshot
		// replaces it wholesale a moment later.
		if snap := c.srv.Snapshots.Load(m.Cluster, m.Kind, m.Namespace); snap != nil {
			c.send(ServerMsg{Type: "snapshot", ID: m.ID, Rows: snap.Rows, Cached: true, CachedAt: snap.SavedAt.Format(time.RFC3339)})
		}
		watch.Resources(subCtx, sess, k, m.Namespace, &sink{c: c, id: m.ID, cluster: m.Cluster, kind: m.Kind, namespace: m.Namespace, srv: c.srv})

	case "unsubscribe":
		c.untrack(m.ID)

	case "events":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			ev, err := watch.EventsFor(ctx, sess, m.Namespace, m.Kind, m.Name)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			if ev == nil {
				ev = []rows.EventRow{}
			}
			c.send(ServerMsg{Type: "events", ID: m.ID, Events: ev})
		}()

	case "consumers":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			list, err := watch.Consumers(ctx, sess, m.Kind, m.Namespace, m.Name)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			if list == nil {
				list = []watch.Consumer{}
			}
			c.send(ServerMsg{Type: "consumers", ID: m.ID, Consumers: list})
		}()

	case "get":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			k, ok := c.kind(ctx, m.ID, m.Cluster, m.Kind)
			if !ok {
				return
			}
			y, err := watch.Get(ctx, sess, k, m.Namespace, m.Name)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			c.send(ServerMsg{Type: "object", ID: m.ID, Object: y})
		}()

	case "history":
		// Name is "pod/<ns>/<name>", "node/<name>" or "cluster".
		sess, ok := c.session(m.ID, m.Cluster)
		if !ok {
			return
		}
		sess.Metrics.EnsureStarted()
		c.send(ServerMsg{Type: "history", ID: m.ID, Samples: sess.Metrics.History(m.Name)})

	case "timeline":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			rep, err := timeline.Scan(ctx, sess, m.Namespace, m.Minutes, c.srv.Audit)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			c.send(ServerMsg{Type: "timeline", ID: m.ID, Timeline: rep})
		}()

	case "drift.scan":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			rep, err := drift.Scan(ctx, sess, m.Path, m.Namespace, m.IncludeUnmanaged)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			c.send(ServerMsg{Type: "drift", ID: m.ID, Drift: rep})
		}()

	case "drift.capture":
		go func() {
			// Nothing here touches a cluster: it writes objects the person is
			// looking at into a repository they chose, on a new branch.
			out, err := drift.CaptureToBranch(ctx, drift.CaptureRequest{
				Repo: m.Path, Dir: m.Dir, Branch: m.Branch, Push: m.Push, Objects: m.Objects,
				IncludeSecretValues: m.IncludeSecretValues,
			})
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			_, _ = c.srv.Audit.Append(audit.Entry{
				Actor: "human", Action: "drift.captured", Cluster: m.Cluster,
				Detail: mustJSON(map[string]any{"repo": out.Repo, "branch": out.Branch, "files": out.Files, "pushed": out.Pushed}),
			})
			c.send(ServerMsg{Type: "capture", ID: m.ID, Capture: out})
		}()

	case "helm.list":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			rels, err := helm.List(ctx, sess, m.Namespace)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			if rels == nil {
				rels = []helm.Release{}
			}
			c.send(ServerMsg{Type: "releases", ID: m.ID, Releases: rels})
		}()

	case "helm.get":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			rel, err := helm.Get(ctx, sess, m.Namespace, m.Name, m.Revision)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			c.send(ServerMsg{Type: "release", ID: m.ID, Release: rel})
		}()

	case "helm.rollback.plan":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			plan, err := helm.PlanRollback(ctx, sess, m.Namespace, m.Name, m.Revision)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			c.send(ServerMsg{Type: "rollback", ID: m.ID, Rollback: plan})
		}()

	case "explain.key":
		explain.SetKey("anthropic", m.Key)
		c.send(ServerMsg{Type: "explain.key", ID: m.ID, HasKey: explain.HasKey("anthropic"), KeyFromEnv: explain.KeyFromEnv("anthropic")})

	case "explain.status":
		c.send(ServerMsg{Type: "explain.key", ID: m.ID, HasKey: explain.HasKey("anthropic"), KeyFromEnv: explain.KeyFromEnv("anthropic")})

	// explain.preview gathers the payload and sends nothing. Everywhere else
	// in this product a person sees what will happen before it happens, and
	// a request that puts cluster data in front of a third party is not the
	// place to make an exception.
	case "explain.preview":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			k, ok := c.kind(ctx, m.ID, m.Cluster, m.Kind)
			if !ok {
				return
			}
			ev, err := explain.Gather(ctx, sess, k, m.Namespace, m.Name)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			c.send(ServerMsg{Type: "explain.preview", ID: m.ID, Evidence: ev})
		}()

	case "explain":
		go func() {
			k, ok := c.kind(ctx, m.ID, m.Cluster, m.Kind)
			if !ok {
				return
			}
			// The reviewed payload comes back with the request, so what
			// leaves this machine is exactly what was on screen.
			ev := m.Evidence
			if ev == nil {
				c.fail(m.ID, "nothing was reviewed: ask for a preview first")
				return
			}
			answer, err := explain.Ask(ctx, k.Kind, m.Namespace, m.Name, ev)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			_, _ = c.srv.Audit.Append(audit.Entry{Actor: "human", Action: "explain.sent", Cluster: m.Cluster,
				Detail: mustJSON(map[string]any{"kind": k.Kind, "namespace": m.Namespace, "name": m.Name, "bytes": len(ev.Object)})})
			c.send(ServerMsg{Type: "explain", ID: m.ID, Answer: answer, Evidence: ev})
		}()

	case "change.propose":
		go func() {
			ch, err := c.srv.Gate.Propose(ctx, m.Cluster, change.Author{Type: "human"}, m.Intent, m.Plan)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			c.send(ServerMsg{Type: "change", ID: m.ID, Change: ch})
		}()

	case "change.decide":
		go func() {
			ch, err := c.srv.Gate.Decide(ctx, m.Name, m.Decision, m.Note)
			if err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			c.send(ServerMsg{Type: "change", ID: m.ID, Change: ch})
		}()

	case "change.list":
		entries, _ := c.srv.Audit.Entries(200)
		c.send(ServerMsg{Type: "changes", ID: m.ID, Changes: c.srv.Gate.List(), Audit: entries})

	case "exec.start":
		sess, ok := c.session(m.ID, m.Cluster)
		if !ok {
			return
		}
		cols, rws := m.Cols, m.Rows
		if cols == 0 {
			cols, rws = 120, 30
		}
		t, err := term.Start(ctx, sess, m.Namespace, m.Name, m.Container, cols, rws, &termSink{c: c, id: m.ID})
		if err != nil {
			c.fail(m.ID, err.Error())
			return
		}
		c.mu.Lock()
		if old, ok := c.terms[m.ID]; ok {
			old.Close()
		}
		c.terms[m.ID] = t
		c.mu.Unlock()
		_, _ = c.srv.Audit.Append(audit.Entry{Actor: "human", Action: "exec.opened", Cluster: m.Cluster, Detail: mustJSON(map[string]any{"namespace": m.Namespace, "pod": m.Name, "container": m.Container})})

	case "exec.input":
		c.mu.Lock()
		t := c.terms[m.ID]
		c.mu.Unlock()
		if t != nil {
			if b, err := base64.StdEncoding.DecodeString(m.Data); err == nil {
				t.Input(b)
			}
		}

	case "exec.resize":
		c.mu.Lock()
		t := c.terms[m.ID]
		c.mu.Unlock()
		if t != nil {
			t.Resize(m.Cols, m.Rows)
		}

	case "exec.stop":
		c.mu.Lock()
		if t, ok := c.terms[m.ID]; ok {
			t.Close()
			delete(c.terms, m.ID)
		}
		c.mu.Unlock()

	case "forward.start":
		go func() {
			sess, ok := c.session(m.ID, m.Cluster)
			if !ok {
				return
			}
			if _, err := c.srv.Forwards.Start(ctx, sess, m.Namespace, m.Name, m.Port, m.LocalPort); err != nil {
				c.fail(m.ID, err.Error())
				return
			}
			c.send(ServerMsg{Type: "forwards", ID: m.ID, Forwards: c.srv.Forwards.List()})
		}()

	case "forward.stop":
		c.srv.Forwards.Stop(m.Name)
		c.send(ServerMsg{Type: "forwards", ID: m.ID, Forwards: c.srv.Forwards.List()})

	case "forward.list":
		c.send(ServerMsg{Type: "forwards", ID: m.ID, Forwards: c.srv.Forwards.List()})

	default:
		c.fail(m.ID, "unknown message type "+m.Type)
	}
}

// mustJSON encodes audit detail, which is always a small map we control.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

type sink struct {
	c         *connection
	id        string
	srv       *Server
	cluster   string
	kind      string
	namespace string
}

func (s *sink) Snapshot(r []rows.Row, metrics bool) {
	if r == nil {
		r = []rows.Row{}
	}
	if s.srv != nil && s.kind != "" {
		s.srv.Snapshots.Save(s.cluster, s.kind, s.namespace, r)
	}
	s.c.send(ServerMsg{Type: "snapshot", ID: s.id, Rows: r, MetricsAvailable: &metrics})
}
func (s *sink) Delta(up []rows.Row, del []string) {
	s.c.send(ServerMsg{Type: "delta", ID: s.id, Upsert: up, Delete: del})
}
func (s *sink) Lines(lines []string, eof bool) {
	if lines == nil {
		lines = []string{}
	}
	s.c.send(ServerMsg{Type: "log", ID: s.id, Lines: lines, EOF: eof})
}
func (s *sink) Error(err error) { s.c.fail(s.id, err.Error()) }

type termSink struct {
	c  *connection
	id string
}

func (t *termSink) Output(b []byte) {
	t.c.send(ServerMsg{Type: "term", ID: t.id, Data: base64.StdEncoding.EncodeToString(b)})
}
func (t *termSink) Closed(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	t.c.send(ServerMsg{Type: "term", ID: t.id, EOF: true, Message: msg})
}

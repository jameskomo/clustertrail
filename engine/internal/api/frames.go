// Package api is the local WebSocket protocol between the UI and the engine.
// One socket, JSON frames, a type field. Subscriptions and terminals are
// multiplexed by id.
package api

import (
	"encoding/json"

	"github.com/jameskomo/clustertrail/engine/internal/audit"
	"github.com/jameskomo/clustertrail/engine/internal/change"
	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/drift"
	"github.com/jameskomo/clustertrail/engine/internal/explain"
	"github.com/jameskomo/clustertrail/engine/internal/forward"
	"github.com/jameskomo/clustertrail/engine/internal/helm"
	"github.com/jameskomo/clustertrail/engine/internal/rows"
	"github.com/jameskomo/clustertrail/engine/internal/timeline"
	"github.com/jameskomo/clustertrail/engine/internal/watch"
)

// ClientMsg is anything the UI sends. Fields are read by Type.
type ClientMsg struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`

	Cluster   string `json:"cluster,omitempty"`
	Kind      string `json:"kind,omitempty"` // registry key: pods, deployments…  (subscribe kind "logs" streams a log)
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`

	// logs
	Container string `json:"container,omitempty"`
	Previous  bool   `json:"previous,omitempty"`
	Tail      int64  `json:"tail,omitempty"`

	// change.propose / change.decide
	Intent   string      `json:"intent,omitempty"`
	Plan     []change.Op `json:"plan,omitempty"`
	Decision string      `json:"decision,omitempty"` // approve | reject
	Note     string      `json:"note,omitempty"`

	// exec
	Data string `json:"data,omitempty"` // base64 bytes
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`

	// forward
	Port      int `json:"port,omitempty"`
	LocalPort int `json:"localPort,omitempty"`

	// drift, helm, explain
	Path                string `json:"path,omitempty"`
	IncludeUnmanaged    bool   `json:"includeUnmanaged,omitempty"`
	IncludeSecretValues bool   `json:"includeSecretValues,omitempty"`
	Dir                 string `json:"dir,omitempty"`
	Branch              string `json:"branch,omitempty"`
	Push                bool   `json:"push,omitempty"`

	Objects  []drift.CaptureObject `json:"objects,omitempty"`
	Revision int                   `json:"revision,omitempty"`
	Minutes  int                   `json:"minutes,omitempty"`
	Provider string                `json:"provider,omitempty"`
	Key      string                `json:"key,omitempty"`
	// Evidence is the payload a person reviewed in the preview, sent back so
	// what leaves the machine is exactly what they were shown.
	Evidence *explain.Evidence `json:"evidence,omitempty"`
}

// ServerMsg is anything the engine sends. Fields are populated by Type.
type ServerMsg struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Cluster string `json:"cluster,omitempty"`
	Message string `json:"message,omitempty"`
	Version string `json:"version,omitempty"`

	Clusters []cluster.Info `json:"clusters,omitempty"`

	Rows             []rows.Row `json:"rows,omitempty"`
	Upsert           []rows.Row `json:"upsert,omitempty"`
	Delete           []string   `json:"delete,omitempty"`
	MetricsAvailable *bool      `json:"metricsAvailable,omitempty"`
	Cached           bool       `json:"cached,omitempty"`
	CachedAt         string     `json:"cachedAt,omitempty"`

	Events []rows.EventRow `json:"events,omitempty"`
	Lines  []string        `json:"lines,omitempty"`
	EOF    bool            `json:"eof,omitempty"`
	Object string          `json:"object,omitempty"` // YAML

	Change  *change.Change  `json:"change,omitempty"`
	Changes []change.Change `json:"changes,omitempty"`
	Audit   []audit.Entry   `json:"audit,omitempty"`

	Samples    []rows.Sample      `json:"samples,omitempty"`
	Drift      *drift.Report      `json:"drift,omitempty"`
	Capture    *drift.Capture     `json:"capture,omitempty"`
	Consumers  []watch.Consumer   `json:"consumers,omitempty"`
	Timeline   *timeline.Report   `json:"timeline,omitempty"`
	Releases   []helm.Release     `json:"releases,omitempty"`
	Release    *helm.Detail       `json:"release,omitempty"`
	Rollback   *helm.RollbackPlan `json:"rollback,omitempty"`
	Answer     string             `json:"answer,omitempty"`
	Evidence   *explain.Evidence  `json:"evidence,omitempty"`
	HasKey     bool               `json:"hasKey,omitempty"`
	KeyFromEnv bool               `json:"keyFromEnv,omitempty"`
	Data       string             `json:"data,omitempty"` // base64 terminal bytes
	Forwards   []forward.Forward  `json:"forwards,omitempty"`

	Extra json.RawMessage `json:"extra,omitempty"`
}

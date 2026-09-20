// Package rows holds the row projections the engine sends to the UI.
// A row is what a table needs and nothing more; the full object is fetched
// on demand when a drawer opens.
package rows

import "time"

// Health is the colour of the status dot.
type Health string

const (
	HealthNone Health = ""
	HealthOK   Health = "ok"
	HealthWarn Health = "warn"
	HealthBad  Health = "bad"
)

// Row is the generic projection. Kind-specific columns live in Cells so the
// UI's table component is one component with a column spec per kind.
type Row struct {
	Key       string         `json:"key"` // namespace/name (or name for cluster-scoped)
	Name      string         `json:"name"`
	Namespace string         `json:"namespace,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	Status    string         `json:"status,omitempty"`
	Health    Health         `json:"health,omitempty"`
	Cells     map[string]any `json:"cells,omitempty"`
}

func Key(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

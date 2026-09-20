package rows

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

// EventRow is one Kubernetes event as the detail drawer shows it.
type EventRow struct {
	Type     string    `json:"type"` // Normal | Warning
	Reason   string    `json:"reason"`
	Message  string    `json:"message"`
	Count    int32     `json:"count"`
	Source   string    `json:"source,omitempty"`
	LastSeen time.Time `json:"lastSeen"`
}

func ProjectEvent(e *corev1.Event) EventRow {
	last := e.LastTimestamp.Time
	if last.IsZero() {
		last = e.EventTime.Time
	}
	if last.IsZero() {
		last = e.CreationTimestamp.Time
	}
	count := e.Count
	if count == 0 && e.Series != nil {
		count = e.Series.Count
	}
	if count == 0 {
		count = 1
	}
	src := e.Source.Component
	if src == "" {
		src = e.ReportingController
	}
	return EventRow{Type: e.Type, Reason: e.Reason, Message: e.Message, Count: count, Source: src, LastSeen: last}
}

package rows

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Sample is one metrics-server reading.
type Sample struct {
	At       time.Time `json:"t"`
	CPUMilli int64     `json:"cpu"`
	MemBytes int64     `json:"mem"`
}

// PodMetrics is what the metrics poller knows about one pod.
type PodMetrics struct {
	CPUMilli int64 `json:"cpu"`
	MemBytes int64 `json:"mem"`
}

// ProjectPod reduces a Pod to its row. The status column reproduces what
// `kubectl get pods` prints so the table never disagrees with the CLI.
func ProjectPod(p *corev1.Pod) Row {
	status := string(p.Status.Phase)
	if p.Status.Reason != "" {
		status = p.Status.Reason
	}
	var restarts int32
	ready := 0
	initializing := false

	for i, cs := range p.Status.InitContainerStatuses {
		restarts += cs.RestartCount
		switch {
		case cs.State.Terminated != nil && cs.State.Terminated.ExitCode == 0:
			continue
		case cs.State.Terminated != nil:
			t := cs.State.Terminated
			if t.Reason == "" {
				status = fmt.Sprintf("Init:ExitCode:%d", t.ExitCode)
			} else {
				status = "Init:" + t.Reason
			}
			initializing = true
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "" && cs.State.Waiting.Reason != "PodInitializing":
			status = "Init:" + cs.State.Waiting.Reason
			initializing = true
		default:
			status = fmt.Sprintf("Init:%d/%d", i, len(p.Spec.InitContainers))
			initializing = true
		}
		break
	}
	if !initializing {
		restarts = 0
		hasRunning := false
		for i := len(p.Status.ContainerStatuses) - 1; i >= 0; i-- {
			cs := p.Status.ContainerStatuses[i]
			restarts += cs.RestartCount
			switch {
			case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
				status = cs.State.Waiting.Reason
			case cs.State.Terminated != nil && cs.State.Terminated.Reason != "":
				status = cs.State.Terminated.Reason
			case cs.State.Terminated != nil:
				t := cs.State.Terminated
				if t.Signal != 0 {
					status = fmt.Sprintf("Signal:%d", t.Signal)
				} else {
					status = fmt.Sprintf("ExitCode:%d", t.ExitCode)
				}
			case cs.Ready && cs.State.Running != nil:
				hasRunning = true
				ready++
			}
		}
		if status == "Completed" && hasRunning {
			status = "Running"
		}
	}
	if p.DeletionTimestamp != nil && p.Status.Reason == "NodeLost" {
		status = "Unknown"
	} else if p.DeletionTimestamp != nil {
		status = "Terminating"
	}

	health := HealthBad
	switch {
	case status == "Running" && ready == len(p.Spec.Containers), status == "Completed", status == "Succeeded":
		health = HealthOK
	case strings.HasPrefix(status, "Init"), status == "Pending", status == "ContainerCreating", status == "PodInitializing", status == "Terminating":
		health = HealthWarn
	}

	names := make([]string, 0, len(p.Spec.Containers))
	for _, c := range p.Spec.Containers {
		names = append(names, c.Name)
	}
	cells := map[string]any{
		"ready":      fmt.Sprintf("%d/%d", ready, len(p.Spec.Containers)),
		"restarts":   restarts,
		"node":       p.Spec.NodeName,
		"ip":         p.Status.PodIP,
		"containers": names,
	}
	if len(p.OwnerReferences) > 0 {
		cells["owner"] = p.OwnerReferences[0].Kind + "/" + p.OwnerReferences[0].Name
	}
	return Row{Key: Key(p.Namespace, p.Name), Name: p.Name, Namespace: p.Namespace, CreatedAt: p.CreationTimestamp.Time, Status: status, Health: health, Cells: cells}
}

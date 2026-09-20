package rows

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func base(m metav1.ObjectMeta) Row {
	return Row{Key: Key(m.Namespace, m.Name), Name: m.Name, Namespace: m.Namespace, CreatedAt: m.CreationTimestamp.Time, Cells: map[string]any{}}
}

func readiness(ready, want int32) (string, Health) {
	if want == 0 {
		return "0/0", HealthWarn
	}
	if ready == want {
		return fmt.Sprintf("%d/%d", ready, want), HealthOK
	}
	if ready == 0 {
		return fmt.Sprintf("%d/%d", ready, want), HealthBad
	}
	return fmt.Sprintf("%d/%d", ready, want), HealthWarn
}

func images(cs []corev1.Container) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Image)
	}
	return out
}

func ProjectDeployment(d *appsv1.Deployment) Row {
	r := base(d.ObjectMeta)
	want := int32(1)
	if d.Spec.Replicas != nil {
		want = *d.Spec.Replicas
	}
	r.Status, r.Health = readiness(d.Status.ReadyReplicas, want)
	if d.Status.UpdatedReplicas < want && want > 0 {
		r.Status += " updating"
	}
	r.Cells["replicas"] = want
	r.Cells["ready"] = d.Status.ReadyReplicas
	r.Cells["updated"] = d.Status.UpdatedReplicas
	r.Cells["available"] = d.Status.AvailableReplicas
	r.Cells["images"] = images(d.Spec.Template.Spec.Containers)
	return r
}

func ProjectStatefulSet(s *appsv1.StatefulSet) Row {
	r := base(s.ObjectMeta)
	want := int32(1)
	if s.Spec.Replicas != nil {
		want = *s.Spec.Replicas
	}
	r.Status, r.Health = readiness(s.Status.ReadyReplicas, want)
	r.Cells["replicas"] = want
	r.Cells["ready"] = s.Status.ReadyReplicas
	r.Cells["images"] = images(s.Spec.Template.Spec.Containers)
	return r
}

func ProjectDaemonSet(d *appsv1.DaemonSet) Row {
	r := base(d.ObjectMeta)
	r.Status, r.Health = readiness(d.Status.NumberReady, d.Status.DesiredNumberScheduled)
	r.Cells["desired"] = d.Status.DesiredNumberScheduled
	r.Cells["ready"] = d.Status.NumberReady
	r.Cells["images"] = images(d.Spec.Template.Spec.Containers)
	return r
}

func ProjectReplicaSet(s *appsv1.ReplicaSet) Row {
	r := base(s.ObjectMeta)
	want := int32(1)
	if s.Spec.Replicas != nil {
		want = *s.Spec.Replicas
	}
	r.Status, r.Health = readiness(s.Status.ReadyReplicas, want)
	if want == 0 {
		r.Health = HealthNone
	}
	r.Cells["replicas"] = want
	r.Cells["ready"] = s.Status.ReadyReplicas
	if len(s.OwnerReferences) > 0 {
		r.Cells["owner"] = s.OwnerReferences[0].Kind + "/" + s.OwnerReferences[0].Name
	}
	return r
}

func ProjectJob(j *batchv1.Job) Row {
	r := base(j.ObjectMeta)
	want := int32(1)
	if j.Spec.Completions != nil {
		want = *j.Spec.Completions
	}
	r.Cells["completions"] = fmt.Sprintf("%d/%d", j.Status.Succeeded, want)
	switch {
	case j.Status.Succeeded >= want:
		r.Status, r.Health = "Complete", HealthOK
	case j.Status.Failed > 0:
		r.Status, r.Health = "Failed", HealthBad
	case j.Status.Active > 0:
		r.Status, r.Health = "Running", HealthWarn
	default:
		r.Status, r.Health = "Pending", HealthWarn
	}
	if j.Status.StartTime != nil && j.Status.CompletionTime != nil {
		r.Cells["duration"] = j.Status.CompletionTime.Sub(j.Status.StartTime.Time).Round(1e9).String()
	}
	return r
}

func ProjectCronJob(c *batchv1.CronJob) Row {
	r := base(c.ObjectMeta)
	r.Cells["schedule"] = c.Spec.Schedule
	r.Cells["active"] = len(c.Status.Active)
	suspended := c.Spec.Suspend != nil && *c.Spec.Suspend
	r.Cells["suspended"] = suspended
	if c.Status.LastScheduleTime != nil {
		r.Cells["lastRun"] = c.Status.LastScheduleTime.Time
	}
	if suspended {
		r.Status, r.Health = "Suspended", HealthWarn
	} else {
		r.Status, r.Health = "Active", HealthOK
	}
	return r
}

func ProjectService(s *corev1.Service) Row {
	r := base(s.ObjectMeta)
	r.Status = string(s.Spec.Type)
	r.Cells["type"] = string(s.Spec.Type)
	r.Cells["clusterIP"] = s.Spec.ClusterIP
	ports := make([]string, 0, len(s.Spec.Ports))
	for _, p := range s.Spec.Ports {
		if p.NodePort != 0 {
			ports = append(ports, fmt.Sprintf("%d:%d/%s", p.Port, p.NodePort, p.Protocol))
		} else {
			ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
		}
	}
	r.Cells["ports"] = strings.Join(ports, ", ")
	ext := []string{}
	for _, i := range s.Status.LoadBalancer.Ingress {
		if i.IP != "" {
			ext = append(ext, i.IP)
		} else if i.Hostname != "" {
			ext = append(ext, i.Hostname)
		}
	}
	r.Cells["external"] = strings.Join(ext, ", ")
	return r
}

func ProjectIngress(i *networkingv1.Ingress) Row {
	r := base(i.ObjectMeta)
	hosts := []string{}
	for _, rule := range i.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	r.Cells["hosts"] = strings.Join(hosts, ", ")
	if i.Spec.IngressClassName != nil {
		r.Cells["class"] = *i.Spec.IngressClassName
	}
	addr := []string{}
	for _, a := range i.Status.LoadBalancer.Ingress {
		if a.IP != "" {
			addr = append(addr, a.IP)
		} else {
			addr = append(addr, a.Hostname)
		}
	}
	r.Cells["address"] = strings.Join(addr, ", ")
	return r
}

func ProjectConfigMap(c *corev1.ConfigMap) Row {
	r := base(c.ObjectMeta)
	keys := make([]string, 0, len(c.Data)+len(c.BinaryData))
	for k := range c.Data {
		keys = append(keys, k)
	}
	for k := range c.BinaryData {
		keys = append(keys, k)
	}
	r.Cells["keys"] = keys
	return r
}

func ProjectSecret(s *corev1.Secret) Row {
	r := base(s.ObjectMeta)
	keys := make([]string, 0, len(s.Data))
	for k := range s.Data {
		keys = append(keys, k)
	}
	r.Cells["keys"] = keys
	r.Cells["type"] = string(s.Type)
	return r
}

func ProjectPVC(p *corev1.PersistentVolumeClaim) Row {
	r := base(p.ObjectMeta)
	r.Status = string(p.Status.Phase)
	r.Health = HealthWarn
	if p.Status.Phase == corev1.ClaimBound {
		r.Health = HealthOK
	}
	if q, ok := p.Status.Capacity[corev1.ResourceStorage]; ok {
		r.Cells["capacity"] = q.String()
	} else if q, ok := p.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		r.Cells["capacity"] = q.String()
	}
	if p.Spec.StorageClassName != nil {
		r.Cells["storageClass"] = *p.Spec.StorageClassName
	}
	r.Cells["volume"] = p.Spec.VolumeName
	return r
}

// NodeMetrics is what the metrics poller knows about one node.
type NodeMetrics struct {
	CPUMilli int64 `json:"cpu"`
	MemBytes int64 `json:"mem"`
}

func ProjectNode(n *corev1.Node) Row {
	r := base(n.ObjectMeta)
	r.Status, r.Health = "NotReady", HealthBad
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			r.Status, r.Health = "Ready", HealthOK
		}
	}
	if n.Spec.Unschedulable {
		r.Status += ",SchedulingDisabled"
		r.Health = HealthWarn
	}
	roles := []string{}
	for k := range n.Labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
		}
	}
	r.Cells["roles"] = strings.Join(roles, ",")
	r.Cells["version"] = n.Status.NodeInfo.KubeletVersion
	r.Cells["os"] = n.Status.NodeInfo.OSImage
	r.Cells["cpuCapacity"] = n.Status.Allocatable.Cpu().MilliValue()
	r.Cells["memCapacity"] = n.Status.Allocatable.Memory().Value()
	for _, a := range n.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			r.Cells["ip"] = a.Address
		}
	}
	return r
}

func ProjectNamespace(n *corev1.Namespace) Row {
	r := base(n.ObjectMeta)
	r.Status = string(n.Status.Phase)
	r.Health = HealthOK
	if n.Status.Phase != corev1.NamespaceActive {
		r.Health = HealthWarn
	}
	return r
}

func ProjectEventRow(e *corev1.Event) Row {
	ev := ProjectEvent(e)
	r := base(e.ObjectMeta)
	r.CreatedAt = ev.LastSeen
	r.Status = e.Type
	r.Health = HealthOK
	if e.Type == corev1.EventTypeWarning {
		r.Health = HealthBad
	}
	r.Cells["reason"] = ev.Reason
	r.Cells["message"] = ev.Message
	r.Cells["count"] = ev.Count
	r.Cells["object"] = e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name
	r.Cells["objectKind"] = e.InvolvedObject.Kind
	r.Cells["objectName"] = e.InvolvedObject.Name
	r.Cells["source"] = ev.Source
	return r
}

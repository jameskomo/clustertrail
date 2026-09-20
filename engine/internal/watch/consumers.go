package watch

import (
	"context"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
)

// Consumer is one workload that uses a Secret or ConfigMap, and how.
type Consumer struct {
	Kind      string `json:"kind"`     // registry key, so the UI can open it
	KindName  string `json:"kindName"` // API kind, for display
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Via       string `json:"via"`    // "environment", "mount", "pull secret", "service account"
	Detail    string `json:"detail"` // which variable, which path
}

// consumerKinds are the workloads worth searching. Pods are included because a
// pod created directly, without a controller, would otherwise be invisible.
var consumerKinds = []string{"deployments", "statefulsets", "daemonsets", "cronjobs", "jobs", "pods", "serviceaccounts"}

// Consumers answers a question every Kubernetes interface makes hard: what
// would break if I changed this Secret?
//
// Without it, the only way to know is to grep every manifest, which misses
// anything applied by hand. This walks the live objects instead.
func Consumers(ctx context.Context, sess *cluster.Session, kindName, namespace, name string) ([]Consumer, error) {
	if kindName != "Secret" && kindName != "ConfigMap" {
		return nil, fmt.Errorf("only Secrets and ConfigMaps have consumers")
	}
	if namespace == "" {
		// A Secret is namespaced, so its consumers are too. An empty
		// namespace here would mean listing seven kinds across the whole
		// cluster to answer a question about one object.
		return nil, fmt.Errorf("a namespace is required to find what uses %s", name)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var found []Consumer
	for _, key := range consumerKinds {
		k, err := kinds.Lookup(key)
		if err != nil {
			continue
		}
		// Paged. Every match is kept and everything else is discarded, so
		// holding the whole namespace in memory at once buys nothing and on
		// a large cluster costs gigabytes in a single burst.
		var items []unstructured.Unstructured
		cont := ""
		for page := 0; page < 40; page++ {
			list, err := sess.Dynamic.Resource(k.GVR).Namespace(namespace).List(ctx, metav1.ListOptions{Limit: 500, Continue: cont})
			if err != nil {
				break
			}
			items = append(items, list.Items...)
			cont = list.GetContinue()
			if cont == "" {
				break
			}
		}
		for i := range items {
			u := &items[i]
			// A pod owned by a controller is covered by its owner's entry.
			if key == "pods" && len(u.GetOwnerReferences()) > 0 {
				continue
			}
			for _, use := range usesOf(u, k.Kind, kindName, name) {
				use.Kind, use.KindName = key, k.Kind
				use.Namespace, use.Name = u.GetNamespace(), u.GetName()
				found = append(found, use)
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Name != found[j].Name {
			return found[i].Name < found[j].Name
		}
		return found[i].Detail < found[j].Detail
	})
	return found, nil
}

// usesOf finds every way one object refers to a Secret or ConfigMap.
func usesOf(u *unstructured.Unstructured, objKind, refKind, refName string) []Consumer {
	var out []Consumer
	add := func(via, detail string) { out = append(out, Consumer{Via: via, Detail: detail}) }

	// A ServiceAccount names Secrets directly.
	if objKind == "ServiceAccount" && refKind == "Secret" {
		for _, field := range []string{"secrets", "imagePullSecrets"} {
			items, _, _ := unstructured.NestedSlice(u.Object, field)
			for _, it := range items {
				if m, ok := it.(map[string]any); ok && m["name"] == refName {
					add("service account", field)
				}
			}
		}
		return out
	}

	spec, ok, _ := unstructured.NestedMap(u.Object, "spec", "template", "spec")
	if !ok {
		// A CronJob buries the pod template one level deeper; a Pod has none.
		if spec, ok, _ = unstructured.NestedMap(u.Object, "spec", "jobTemplate", "spec", "template", "spec"); !ok {
			spec, _, _ = unstructured.NestedMap(u.Object, "spec")
		}
	}
	if spec == nil {
		return out
	}

	envRef, envFromRef := "secretKeyRef", "secretRef"
	if refKind == "ConfigMap" {
		envRef, envFromRef = "configMapKeyRef", "configMapRef"
	}

	containers, _ := spec["containers"].([]any)
	initContainers, _ := spec["initContainers"].([]any)
	for _, c := range append(append([]any{}, initContainers...), containers...) {
		m, _ := c.(map[string]any)
		cname := fmt.Sprint(m["name"])
		env, _ := m["env"].([]any)
		for _, e := range env {
			em, _ := e.(map[string]any)
			vf, _ := em["valueFrom"].(map[string]any)
			ref, _ := vf[envRef].(map[string]any)
			if ref != nil && ref["name"] == refName {
				add("environment", fmt.Sprintf("%s reads %v from key %v", cname, em["name"], ref["key"]))
			}
		}
		from, _ := m["envFrom"].([]any)
		for _, fr := range from {
			fm, _ := fr.(map[string]any)
			ref, _ := fm[envFromRef].(map[string]any)
			if ref != nil && ref["name"] == refName {
				add("environment", fmt.Sprintf("%s reads every key", cname))
			}
		}
	}

	// Which volume names refer to it, and where each is mounted.
	backing := map[string]bool{}
	vols, _ := spec["volumes"].([]any)
	for _, v := range vols {
		vm, _ := v.(map[string]any)
		name := fmt.Sprint(vm["name"])
		if refKind == "Secret" {
			if s, ok := vm["secret"].(map[string]any); ok && s["secretName"] == refName {
				backing[name] = true
			}
		} else if cm, ok := vm["configMap"].(map[string]any); ok && cm["name"] == refName {
			backing[name] = true
		}
		// A projected volume can carry either.
		if p, ok := vm["projected"].(map[string]any); ok {
			sources, _ := p["sources"].([]any)
			for _, src := range sources {
				sm, _ := src.(map[string]any)
				key := "secret"
				if refKind == "ConfigMap" {
					key = "configMap"
				}
				if r, ok := sm[key].(map[string]any); ok && r["name"] == refName {
					backing[name] = true
				}
			}
		}
	}
	if len(backing) > 0 {
		for _, c := range append(append([]any{}, initContainers...), containers...) {
			m, _ := c.(map[string]any)
			cname := fmt.Sprint(m["name"])
			mounts, _ := m["volumeMounts"].([]any)
			for _, mt := range mounts {
				mm, _ := mt.(map[string]any)
				if backing[fmt.Sprint(mm["name"])] {
					add("mount", fmt.Sprintf("%s at %v", cname, mm["mountPath"]))
				}
			}
		}
	}

	if refKind == "Secret" {
		pulls, _ := spec["imagePullSecrets"].([]any)
		for _, p := range pulls {
			pm, _ := p.(map[string]any)
			if pm["name"] == refName {
				add("pull secret", "used to pull images")
			}
		}
	}
	return out
}

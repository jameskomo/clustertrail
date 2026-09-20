// Package kinds is the registry of resource kinds the engine understands:
// the GVR to watch, whether it is namespaced, and how to project it to a row.
// Adding a kind is adding an entry here and a column spec in the UI.
package kinds

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/jameskomo/clustertrail/engine/internal/rows"
)

type Kind struct {
	Key        string
	Kind       string // API kind name, for events and YAML
	GVR        schema.GroupVersionResource
	Namespaced bool
	Project    func(*unstructured.Unstructured) (rows.Row, error)
	Scalable   bool
	Restarts   bool     // supports rollout restart
	Columns    []string // custom resources: extra printer columns, in order
}

func typed[T any](project func(*T) rows.Row) func(*unstructured.Unstructured) (rows.Row, error) {
	return func(u *unstructured.Unstructured) (rows.Row, error) {
		var t T
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &t); err != nil {
			return rows.Row{}, err
		}
		return project(&t), nil
	}
}

var registry = map[string]Kind{}

func register(k Kind) { registry[k.Key] = k }

func init() {
	core := func(r string) schema.GroupVersionResource {
		return schema.GroupVersionResource{Version: "v1", Resource: r}
	}
	apps := func(r string) schema.GroupVersionResource {
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: r}
	}
	register(Kind{Key: "pods", Kind: "Pod", GVR: core("pods"), Namespaced: true, Project: typed(rows.ProjectPod)})
	register(Kind{Key: "deployments", Kind: "Deployment", GVR: apps("deployments"), Namespaced: true, Project: typed(rows.ProjectDeployment), Scalable: true, Restarts: true})
	register(Kind{Key: "statefulsets", Kind: "StatefulSet", GVR: apps("statefulsets"), Namespaced: true, Project: typed(rows.ProjectStatefulSet), Scalable: true, Restarts: true})
	register(Kind{Key: "daemonsets", Kind: "DaemonSet", GVR: apps("daemonsets"), Namespaced: true, Project: typed(rows.ProjectDaemonSet), Restarts: true})
	register(Kind{Key: "replicasets", Kind: "ReplicaSet", GVR: apps("replicasets"), Namespaced: true, Project: typed(rows.ProjectReplicaSet), Scalable: true})
	register(Kind{Key: "jobs", Kind: "Job", GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, Namespaced: true, Project: typed(rows.ProjectJob)})
	register(Kind{Key: "cronjobs", Kind: "CronJob", GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, Namespaced: true, Project: typed(rows.ProjectCronJob)})
	register(Kind{Key: "services", Kind: "Service", GVR: core("services"), Namespaced: true, Project: typed(rows.ProjectService)})
	register(Kind{Key: "ingresses", Kind: "Ingress", GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, Namespaced: true, Project: typed(rows.ProjectIngress)})
	register(Kind{Key: "configmaps", Kind: "ConfigMap", GVR: core("configmaps"), Namespaced: true, Project: typed(rows.ProjectConfigMap)})
	register(Kind{Key: "secrets", Kind: "Secret", GVR: core("secrets"), Namespaced: true, Project: typed(rows.ProjectSecret)})
	register(Kind{Key: "persistentvolumeclaims", Kind: "PersistentVolumeClaim", GVR: core("persistentvolumeclaims"), Namespaced: true, Project: typed(rows.ProjectPVC)})
	register(Kind{Key: "events", Kind: "Event", GVR: core("events"), Namespaced: true, Project: typed(rows.ProjectEventRow)})
	register(Kind{Key: "nodes", Kind: "Node", GVR: core("nodes"), Namespaced: false, Project: typed(rows.ProjectNode)})
	register(Kind{Key: "namespaces", Kind: "Namespace", GVR: core("namespaces"), Namespaced: false, Project: typed(rows.ProjectNamespace)})
	rbac := func(r string) schema.GroupVersionResource {
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: r}
	}
	register(Kind{Key: "serviceaccounts", Kind: "ServiceAccount", GVR: core("serviceaccounts"), Namespaced: true, Project: typed(rows.ProjectServiceAccount)})
	register(Kind{Key: "roles", Kind: "Role", GVR: rbac("roles"), Namespaced: true, Project: typed(rows.ProjectRole)})
	register(Kind{Key: "rolebindings", Kind: "RoleBinding", GVR: rbac("rolebindings"), Namespaced: true, Project: typed(rows.ProjectRoleBinding)})
	register(Kind{Key: "clusterroles", Kind: "ClusterRole", GVR: rbac("clusterroles"), Namespaced: false, Project: typed(rows.ProjectClusterRole)})
	register(Kind{Key: "clusterrolebindings", Kind: "ClusterRoleBinding", GVR: rbac("clusterrolebindings"), Namespaced: false, Project: typed(rows.ProjectClusterRoleBinding)})
	register(Kind{Key: "customresourcedefinitions", Kind: "CustomResourceDefinition", GVR: schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}, Namespaced: false, Project: typed(rows.ProjectCRD)})
}

// Custom builds a Kind for one CRD version. Keys look like
// "crd:certificates.cert-manager.io" so they never collide with built-ins.
func Custom(crd *apiextv1.CustomResourceDefinition) (Kind, bool) {
	var ver *apiextv1.CustomResourceDefinitionVersion
	for i := range crd.Spec.Versions {
		v := &crd.Spec.Versions[i]
		if v.Served && (ver == nil || v.Storage) {
			ver = v
		}
	}
	if ver == nil {
		return Kind{}, false
	}
	cols := rows.CompileColumns(ver.AdditionalPrinterColumns)
	project := rows.ProjectCustom(cols)
	return Kind{
		Key:        "crd:" + crd.Name,
		Kind:       crd.Spec.Names.Kind,
		GVR:        schema.GroupVersionResource{Group: crd.Spec.Group, Version: ver.Name, Resource: crd.Spec.Names.Plural},
		Namespaced: crd.Spec.Scope == apiextv1.NamespaceScoped,
		Project:    func(u *unstructured.Unstructured) (rows.Row, error) { return project(u), nil },
		Columns:    columnNames(cols),
	}, true
}

func columnNames(cols []rows.PrinterColumn) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

// Silence unused-import checks for typed helpers referenced only via generics.
var (
	_ = appsv1.Deployment{}
	_ = batchv1.Job{}
	_ = corev1.Pod{}
	_ = networkingv1.Ingress{}
	_ = rbacv1.Role{}
)

// Lookup accepts a registry key ("deployments") or an API kind ("Deployment").
func Lookup(key string) (Kind, error) {
	if k, ok := registry[key]; ok {
		return k, nil
	}
	if k, ok := ByKindName(key); ok {
		return k, nil
	}
	return Kind{}, fmt.Errorf("unknown kind %q", key)
}

// Keys lists every registered kind key.
func Keys() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// ByKindName finds a registered kind by its API kind name (e.g. "Deployment").
func ByKindName(kind string) (Kind, bool) {
	for _, k := range registry {
		if k.Kind == kind {
			return k, true
		}
	}
	return Kind{}, false
}

var crdGVR = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}

// CustomFor fetches a CRD by name and builds its Kind.
func CustomFor(ctx context.Context, dyn dynamic.Interface, name string) (Kind, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u, err := dyn.Resource(crdGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Kind{}, err
	}
	var crd apiextv1.CustomResourceDefinition
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &crd); err != nil {
		return Kind{}, err
	}
	k, ok := Custom(&crd)
	if !ok {
		return Kind{}, fmt.Errorf("%s has no served version", name)
	}
	return k, nil
}

package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/jameskomo/clustertrail/engine/internal/drift"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
	"github.com/jameskomo/clustertrail/engine/internal/timeline"
	"github.com/jameskomo/clustertrail/engine/internal/watch"
)

// tools is the entire surface an agent gets. Every one of them reads.
var tools = []tool{
	{
		Name: "list_clusters", Title: "List clusters",
		Description: "List the Kubernetes contexts in this machine's kubeconfig. Nothing connects until another tool names one.",
		InputSchema: schema(map[string]any{}),
		run: func(ctx context.Context, s *server, args map[string]any) (string, error) {
			var b strings.Builder
			for _, c := range s.reg.Clusters() {
				mark := " "
				if c.Current {
					mark = "*"
				}
				fmt.Fprintf(&b, "%s %s  %s\n", mark, c.Name, c.Server)
			}
			if b.Len() == 0 {
				return "No contexts found in the kubeconfig.", nil
			}
			return "* marks the current context.\n\n" + b.String(), nil
		},
	},
	{
		Name: "list_kinds", Title: "List resource kinds",
		Description: "List the resource kinds this server can read, with the key to pass as `kind`.",
		InputSchema: schema(map[string]any{}),
		run: func(ctx context.Context, s *server, args map[string]any) (string, error) {
			keys := kinds.Keys()
			sort.Strings(keys)
			return strings.Join(keys, "\n") + "\n\nPass a CRD as \"crd:<crd name>\" to read a custom resource.", nil
		},
	},
	{
		Name: "list_resources", Title: "List resources",
		Description: "List objects of one kind as a table, the way `kubectl get` prints them.",
		InputSchema: schema(map[string]any{
			"kind":      strProp("Resource kind key, for example \"pods\" or \"deployments\". Use list_kinds."),
			"namespace": strProp("Namespace. Omit for all namespaces."),
			"cluster":   strProp(clusterDesc),
		}, "kind"),
		run: func(ctx context.Context, s *server, args map[string]any) (string, error) {
			sess, err := s.session(args)
			if err != nil {
				return "", err
			}
			k, err := kinds.Lookup(str(args, "kind"))
			if err != nil {
				return "", err
			}
			ns := str(args, "namespace")
			if !k.Namespaced {
				ns = ""
			}
			list, err := sess.Dynamic.Resource(k.GVR).Namespace(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d %s\n\n", len(list.Items), k.Kind)
			for i := range list.Items {
				row, err := k.Project(&list.Items[i])
				if err != nil {
					continue
				}
				cells := make([]string, 0, len(row.Cells))
				for key, v := range row.Cells {
					if key == "containers" || key == "keys" || key == "subjects" {
						continue
					}
					cells = append(cells, fmt.Sprintf("%s=%v", key, v))
				}
				sort.Strings(cells)
				fmt.Fprintf(&b, "%s  %s  %s\n", row.Key, row.Status, strings.Join(cells, " "))
			}
			return b.String(), nil
		},
	},
	{
		Name: "get_resource", Title: "Get one resource",
		Description: "Fetch one object as YAML, with managedFields removed. Secret values are replaced with a placeholder; the keys remain.",
		InputSchema: schema(map[string]any{
			"kind":      strProp("Resource kind key."),
			"name":      strProp("Object name."),
			"namespace": strProp("Namespace, for namespaced kinds."),
			"cluster":   strProp(clusterDesc),
		}, "kind", "name"),
		run: func(ctx context.Context, s *server, args map[string]any) (string, error) {
			sess, err := s.session(args)
			if err != nil {
				return "", err
			}
			k, err := kinds.Lookup(str(args, "kind"))
			if err != nil {
				return "", err
			}
			// Redacted: this text goes to a model, not to the owner.
			return watch.GetRedacted(ctx, sess, k, str(args, "namespace"), str(args, "name"))
		},
	},
	{
		Name: "get_events", Title: "Get events for an object",
		Description: "Recent Kubernetes events for one object, newest first. Events expire after about an hour.",
		InputSchema: schema(map[string]any{
			"kind":      strProp("API kind, for example \"Pod\" or \"Deployment\"."),
			"name":      strProp("Object name."),
			"namespace": strProp("Namespace."),
			"cluster":   strProp(clusterDesc),
		}, "name", "namespace"),
		run: func(ctx context.Context, s *server, args map[string]any) (string, error) {
			sess, err := s.session(args)
			if err != nil {
				return "", err
			}
			evs, err := watch.EventsFor(ctx, sess, str(args, "namespace"), str(args, "kind"), str(args, "name"))
			if err != nil {
				return "", err
			}
			if len(evs) == 0 {
				return "No events in the last hour.", nil
			}
			var b strings.Builder
			for _, e := range evs {
				fmt.Fprintf(&b, "%s %s x%d (%s): %s\n", e.LastSeen.Format("15:04:05"), e.Reason, e.Count, e.Type, e.Message)
			}
			return b.String(), nil
		},
	},
	{
		Name: "get_logs", Title: "Get pod logs",
		Description: "The last lines of a pod's logs. Set previous to read what a crashed container printed before it died.",
		InputSchema: schema(map[string]any{
			"name":      strProp("Pod name."),
			"namespace": strProp("Namespace."),
			"lines":     numProp("How many lines per container. Default 120."),
			"cluster":   strProp(clusterDesc),
		}, "name", "namespace"),
		run: func(ctx context.Context, s *server, args map[string]any) (string, error) {
			sess, err := s.session(args)
			if err != nil {
				return "", err
			}
			lines := watch.TailLogs(ctx, sess, str(args, "namespace"), str(args, "name"), int64(num(args, "lines", 120)))
			if len(lines) == 0 {
				return "No log output. The container may not have started.", nil
			}
			return strings.Join(lines, "\n"), nil
		},
	},
	{
		Name: "what_changed", Title: "What changed recently",
		Description: "Rollouts, field writes recorded in managedFields, notable cluster events, and changes made through ClusterTrail, merged into one ordered list. This is the first thing to read during an incident.",
		InputSchema: schema(map[string]any{
			"minutes":   numProp("How far back to look. Default 180."),
			"namespace": strProp("Namespace. Omit for the whole cluster."),
			"cluster":   strProp(clusterDesc),
		}),
		run: func(ctx context.Context, s *server, args map[string]any) (string, error) {
			sess, err := s.session(args)
			if err != nil {
				return "", err
			}
			rep, err := timeline.Scan(ctx, sess, str(args, "namespace"), num(args, "minutes", 180), nil)
			if err != nil {
				return "", err
			}
			if len(rep.Items) == 0 {
				return fmt.Sprintf("Nothing changed in the last %d minutes.", rep.Minutes), nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d changes in the last %d minutes\n\n", len(rep.Items), rep.Minutes)
			for _, i := range rep.Items {
				fmt.Fprintf(&b, "%s  %-8s %s/%s  %s", i.At.Format("15:04:05"), i.Source, i.KindName, i.Name, i.Summary)
				if i.Actor != "" {
					fmt.Fprintf(&b, "  [%s]", i.Actor)
				}
				if i.Detail != "" {
					fmt.Fprintf(&b, "\n    %s", i.Detail)
				}
				b.WriteString("\n")
			}
			return b.String(), nil
		},
	},
	{
		Name: "drift_report", Title: "Compare a repository with a cluster",
		Description: "Compare a directory of Kubernetes YAML with the live cluster. Reports what is in the repository but not running, what differs, and optionally what runs with no file behind it. It takes the diff with a server-side dry-run update, so it needs update permission and runs admission webhooks, and it stores nothing. Secret values in the diff are replaced with a placeholder.",
		InputSchema: schema(map[string]any{
			"path":      strProp("Absolute path to the directory of YAML."),
			"namespace": strProp("Default namespace for objects that do not name one."),
			"unmanaged": boolProp("Also report objects running with no file behind them."),
			"cluster":   strProp(clusterDesc),
		}, "path"),
		run: func(ctx context.Context, s *server, args map[string]any) (string, error) {
			sess, err := s.session(args)
			if err != nil {
				return "", err
			}
			unmanaged, _ := args["unmanaged"].(bool)
			rep, err := drift.Scan(ctx, sess, str(args, "path"), str(args, "namespace"), unmanaged)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d files. missing=%d modified=%d unmanaged=%d in-sync=%d\n\n",
				rep.Files, rep.Summary.Missing, rep.Summary.Modified, rep.Summary.Unmanaged, rep.Summary.InSync)
			for _, i := range rep.Items {
				if i.State == drift.InSync {
					continue
				}
				fmt.Fprintf(&b, "%-10s %s/%s %s\n", i.State, i.KindName, i.Name, i.File)
				if i.Diff != "" {
					fmt.Fprintf(&b, "%s\n", i.Diff)
				}
			}
			return b.String(), nil
		},
	},
	{
		Name: "describe_containers", Title: "Describe a workload's containers",
		Description: "For a pod or workload: each container with its image, ports, resources, probes, the source of every environment variable, and what backs each mount. Secret values are named, never returned.",
		InputSchema: schema(map[string]any{
			"kind":      strProp("Resource kind key, for example \"pods\" or \"deployments\"."),
			"name":      strProp("Object name."),
			"namespace": strProp("Namespace."),
			"cluster":   strProp(clusterDesc),
		}, "kind", "name", "namespace"),
		run: func(ctx context.Context, s *server, args map[string]any) (string, error) {
			sess, err := s.session(args)
			if err != nil {
				return "", err
			}
			k, err := kinds.Lookup(str(args, "kind"))
			if err != nil {
				return "", err
			}
			u, err := sess.Dynamic.Resource(k.GVR).Namespace(str(args, "namespace")).Get(ctx, str(args, "name"), metav1.GetOptions{})
			if err != nil {
				return "", err
			}
			return describeContainers(u), nil
		},
	},
}

// describeContainers renders the pod spec the way the desktop drawer does, in
// text a model can read. Secret values are deliberately not resolved: the tool
// names the Secret and the key, and a person can look it up.
func describeContainers(u *unstructured.Unstructured) string {
	spec, ok, _ := unstructured.NestedMap(u.Object, "spec", "template", "spec")
	if !ok {
		spec, _, _ = unstructured.NestedMap(u.Object, "spec")
	}
	containers, _ := spec["containers"].([]any)
	initContainers, _ := spec["initContainers"].([]any)
	if len(containers)+len(initContainers) == 0 {
		return "No containers found on this object."
	}
	backing := volumeBacking(spec)

	var b strings.Builder
	render := func(c any, prefix string) {
		m, _ := c.(map[string]any)
		fmt.Fprintf(&b, "%scontainer %v\n  image %v\n", prefix, m["name"], m["image"])
		if cmd := joinStrings(m["command"], m["args"]); cmd != "" {
			fmt.Fprintf(&b, "  command %s\n", cmd)
		}
		if ports, ok := m["ports"].([]any); ok {
			for _, p := range ports {
				pm, _ := p.(map[string]any)
				fmt.Fprintf(&b, "  port %v/%v", pm["containerPort"], orDefault(pm["protocol"], "TCP"))
				if pm["name"] != nil {
					fmt.Fprintf(&b, " named %v", pm["name"])
				}
				b.WriteString("\n")
			}
		}
		if r, ok := m["resources"].(map[string]any); ok {
			if req := quantities(r["requests"]); req != "" {
				fmt.Fprintf(&b, "  requests %s\n", req)
			}
			if lim := quantities(r["limits"]); lim != "" {
				fmt.Fprintf(&b, "  limits %s\n", lim)
			}
		}
		if env, ok := m["env"].([]any); ok {
			for _, e := range env {
				em, _ := e.(map[string]any)
				fmt.Fprintf(&b, "  env %v %s\n", em["name"], envSource(em))
			}
		}
		if from, ok := m["envFrom"].([]any); ok {
			for _, f := range from {
				fm, _ := f.(map[string]any)
				if r, ok := fm["secretRef"].(map[string]any); ok {
					fmt.Fprintf(&b, "  env all of secret %v\n", r["name"])
				} else if r, ok := fm["configMapRef"].(map[string]any); ok {
					fmt.Fprintf(&b, "  env all of configmap %v\n", r["name"])
				}
			}
		}
		if mounts, ok := m["volumeMounts"].([]any); ok {
			for _, mt := range mounts {
				mm, _ := mt.(map[string]any)
				name := fmt.Sprint(mm["name"])
				ro := ""
				if mm["readOnly"] == true {
					ro = " (read only)"
				}
				fmt.Fprintf(&b, "  mount %v <- %s%s\n", mm["mountPath"], orDefault(backing[name], "volume "+name), ro)
			}
		}
		for _, probe := range []string{"livenessProbe", "readinessProbe", "startupProbe"} {
			if p, ok := m[probe].(map[string]any); ok {
				fmt.Fprintf(&b, "  %s %s\n", strings.TrimSuffix(probe, "Probe"), describeProbe(p))
			}
		}
		b.WriteString("\n")
	}
	for _, c := range initContainers {
		render(c, "init ")
	}
	for _, c := range containers {
		render(c, "")
	}
	return b.String()
}

// volumeBacking maps a volume name to what actually backs it, so a mount reads
// as "secret app-credentials" rather than a name that means nothing on its own.
func volumeBacking(spec map[string]any) map[string]string {
	out := map[string]string{}
	vols, _ := spec["volumes"].([]any)
	for _, v := range vols {
		vm, _ := v.(map[string]any)
		name := fmt.Sprint(vm["name"])
		switch {
		case vm["secret"] != nil:
			out[name] = fmt.Sprintf("secret %v", vm["secret"].(map[string]any)["secretName"])
		case vm["configMap"] != nil:
			out[name] = fmt.Sprintf("configmap %v", vm["configMap"].(map[string]any)["name"])
		case vm["persistentVolumeClaim"] != nil:
			out[name] = fmt.Sprintf("claim %v", vm["persistentVolumeClaim"].(map[string]any)["claimName"])
		case vm["emptyDir"] != nil:
			out[name] = "emptyDir"
		case vm["hostPath"] != nil:
			out[name] = fmt.Sprintf("hostPath %v", vm["hostPath"].(map[string]any)["path"])
		case vm["projected"] != nil:
			out[name] = "projected"
		}
	}
	return out
}

// envSource says where a value comes from, in the same words the desktop uses.
func envSource(em map[string]any) string {
	if v, ok := em["value"]; ok && v != nil {
		return fmt.Sprintf("= %v", v)
	}
	vf, _ := em["valueFrom"].(map[string]any)
	switch {
	case vf["secretKeyRef"] != nil:
		r := vf["secretKeyRef"].(map[string]any)
		return fmt.Sprintf("from secret %v.%v (value not returned)", r["name"], r["key"])
	case vf["configMapKeyRef"] != nil:
		r := vf["configMapKeyRef"].(map[string]any)
		return fmt.Sprintf("from configmap %v.%v", r["name"], r["key"])
	case vf["fieldRef"] != nil:
		return fmt.Sprintf("from field %v", vf["fieldRef"].(map[string]any)["fieldPath"])
	case vf["resourceFieldRef"] != nil:
		return fmt.Sprintf("from resource %v", vf["resourceFieldRef"].(map[string]any)["resource"])
	}
	return "unresolved"
}

func describeProbe(p map[string]any) string {
	what := "probe"
	if g, ok := p["httpGet"].(map[string]any); ok {
		what = fmt.Sprintf("GET %v:%v", orDefault(g["path"], "/"), g["port"])
	} else if e, ok := p["exec"].(map[string]any); ok {
		what = "exec " + joinStrings(e["command"], nil)
	} else if t, ok := p["tcpSocket"].(map[string]any); ok {
		what = fmt.Sprintf("tcp %v", t["port"])
	}
	return fmt.Sprintf("%s every %vs", what, orDefault(p["periodSeconds"], int64(10)))
}

func quantities(v any) string {
	m, _ := v.(map[string]any)
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %v", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func joinStrings(lists ...any) string {
	var out []string
	for _, l := range lists {
		items, _ := l.([]any)
		for _, i := range items {
			out = append(out, fmt.Sprint(i))
		}
	}
	return strings.Join(out, " ")
}

func orDefault(v any, def any) any {
	if v == nil || v == "" {
		return def
	}
	return v
}

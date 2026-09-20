package rows

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

func ruleSummary(rules []rbacv1.PolicyRule) string {
	verbs, res := map[string]bool{}, map[string]bool{}
	for _, r := range rules {
		for _, v := range r.Verbs {
			verbs[v] = true
		}
		for _, x := range r.Resources {
			res[x] = true
		}
	}
	vs := make([]string, 0, len(verbs))
	for v := range verbs {
		vs = append(vs, v)
	}
	rs := make([]string, 0, len(res))
	for r := range res {
		rs = append(rs, r)
	}
	if verbs["*"] && res["*"] {
		return "everything"
	}
	return fmt.Sprintf("%d rules: %s on %s", len(rules), strings.Join(vs, ","), strings.Join(rs, ","))
}

func subjects(ss []rbacv1.Subject) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s.Namespace != "" {
			out = append(out, fmt.Sprintf("%s %s/%s", s.Kind, s.Namespace, s.Name))
		} else {
			out = append(out, fmt.Sprintf("%s %s", s.Kind, s.Name))
		}
	}
	return out
}

func ProjectRole(r *rbacv1.Role) Row {
	row := base(r.ObjectMeta)
	row.Cells["rules"] = len(r.Rules)
	row.Cells["summary"] = ruleSummary(r.Rules)
	return row
}

func ProjectClusterRole(r *rbacv1.ClusterRole) Row {
	row := base(r.ObjectMeta)
	row.Cells["rules"] = len(r.Rules)
	row.Cells["summary"] = ruleSummary(r.Rules)
	if r.AggregationRule != nil {
		row.Cells["summary"] = "aggregated"
	}
	return row
}

func ProjectRoleBinding(b *rbacv1.RoleBinding) Row {
	row := base(b.ObjectMeta)
	row.Cells["role"] = b.RoleRef.Kind + "/" + b.RoleRef.Name
	row.Cells["subjects"] = subjects(b.Subjects)
	return row
}

func ProjectClusterRoleBinding(b *rbacv1.ClusterRoleBinding) Row {
	row := base(b.ObjectMeta)
	row.Cells["role"] = b.RoleRef.Kind + "/" + b.RoleRef.Name
	row.Cells["subjects"] = subjects(b.Subjects)
	return row
}

func ProjectServiceAccount(s *corev1.ServiceAccount) Row {
	row := base(s.ObjectMeta)
	secrets := make([]string, 0, len(s.Secrets))
	for _, x := range s.Secrets {
		secrets = append(secrets, x.Name)
	}
	row.Cells["secrets"] = secrets
	return row
}

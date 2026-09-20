package rows

import (
	"bytes"
	"fmt"
	"strings"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/jsonpath"
)

// PrinterColumn is one additionalPrinterColumns entry from a CRD, compiled.
type PrinterColumn struct {
	Name string
	Type string
	path *jsonpath.JSONPath
}

// CompileColumns turns a CRD version's printer columns into evaluators.
// maxColumns and maxCell bound what a CRD can put in a table. Both the
// number of printer columns and the JSONPath behind each one are chosen by
// whoever installed the CRD, and a path like {.metadata.annotations}
// serialises an arbitrarily large subtree into every row, on every event.
const (
	maxColumns = 12
	maxCell    = 256
)

func CompileColumns(cols []apiextv1.CustomResourceColumnDefinition) []PrinterColumn {
	out := make([]PrinterColumn, 0, len(cols))
	for _, c := range cols {
		if len(out) >= maxColumns {
			break
		}
		if c.JSONPath == ".metadata.creationTimestamp" || c.Name == "Age" {
			continue // the table already has age
		}
		jp := jsonpath.New(c.Name)
		jp.AllowMissingKeys(true)
		if err := jp.Parse("{" + c.JSONPath + "}"); err != nil {
			continue
		}
		out = append(out, PrinterColumn{Name: c.Name, Type: c.Type, path: jp})
	}
	return out
}

// ProjectCustom is the projection for any custom resource: the standard
// columns plus whatever the CRD author declared, exactly as kubectl shows it.
func ProjectCustom(cols []PrinterColumn) func(*unstructured.Unstructured) Row {
	return func(u *unstructured.Unstructured) Row {
		r := Row{Key: Key(u.GetNamespace(), u.GetName()), Name: u.GetName(), Namespace: u.GetNamespace(), CreatedAt: u.GetCreationTimestamp().Time, Cells: map[string]any{}}
		for _, c := range cols {
			var buf bytes.Buffer
			if err := c.path.Execute(&buf, u.Object); err == nil {
				cell := strings.TrimSpace(buf.String())
				if len(cell) > maxCell {
					cell = cell[:maxCell] + "…"
				}
				r.Cells[c.Name] = cell
			}
		}
		// A conventional status.conditions Ready=True lights the dot green.
		if conds, ok, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); ok {
			for _, c := range conds {
				m, _ := c.(map[string]any)
				if fmt.Sprint(m["type"]) == "Ready" {
					if fmt.Sprint(m["status"]) == "True" {
						r.Health, r.Status = HealthOK, "Ready"
					} else {
						r.Health, r.Status = HealthBad, fmt.Sprint(m["reason"])
					}
				}
			}
		}
		return r
	}
}

// ProjectCRD lists the definitions themselves.
func ProjectCRD(c *apiextv1.CustomResourceDefinition) Row {
	r := base(c.ObjectMeta)
	r.Cells["group"] = c.Spec.Group
	r.Cells["kind"] = c.Spec.Names.Kind
	r.Cells["scope"] = string(c.Spec.Scope)
	vs := make([]string, 0, len(c.Spec.Versions))
	for _, v := range c.Spec.Versions {
		if v.Served {
			vs = append(vs, v.Name)
		}
	}
	r.Cells["versions"] = vs
	r.Health = HealthOK
	for _, cond := range c.Status.Conditions {
		if cond.Type == apiextv1.Established && cond.Status != apiextv1.ConditionTrue {
			r.Health, r.Status = HealthWarn, "Not established"
		}
	}
	if r.Status == "" {
		r.Status = "Established"
	}
	return r
}

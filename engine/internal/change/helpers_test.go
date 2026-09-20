package change

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

func object(m map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: m}
}

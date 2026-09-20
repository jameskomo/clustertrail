package drift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const deployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
  namespace: shop
spec:
  replicas: 2
`

func TestRenderReadsEveryYAMLFileAndSkipsTheRest(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "deploy.yaml", deployment)
	write(t, dir, "nested/service.yml", "apiVersion: v1\nkind: Service\nmetadata:\n  name: web\n")
	write(t, dir, "values.json", `{"not":"kubernetes"}`)
	write(t, dir, "README.md", "# not yaml")
	// A Helm values file is YAML but not a Kubernetes object, and must not be
	// mistaken for one.
	write(t, dir, "values.yaml", "replicaCount: 3\nimage:\n  tag: latest\n")

	objs, files, err := render(dir, "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if files != 3 {
		t.Errorf("files read: want 3 yaml files, got %d", files)
	}
	if len(objs) != 2 {
		t.Fatalf("objects: want 2 Kubernetes objects, got %d", len(objs))
	}
}

func TestRenderSkipsVendorAndGitDirectories(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "deploy.yaml", deployment)
	write(t, dir, ".git/config.yaml", deployment)
	write(t, dir, "vendor/chart.yaml", deployment)
	write(t, dir, "node_modules/thing.yaml", deployment)

	objs, _, err := render(dir, "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("want only the top-level object, got %d", len(objs))
	}
}

func TestRenderSplitsMultiDocumentFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "all.yaml", deployment+"---\n"+"apiVersion: v1\nkind: Service\nmetadata:\n  name: web\n  namespace: shop\n")
	objs, files, err := render(dir, "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if files != 1 {
		t.Errorf("files: want 1, got %d", files)
	}
	if len(objs) != 2 {
		t.Fatalf("want both documents, got %d", len(objs))
	}
	if objs[0].kind.Kind != "Deployment" || objs[1].kind.Kind != "Service" {
		t.Errorf("wrong kinds: %s, %s", objs[0].kind.Kind, objs[1].kind.Kind)
	}
}

func TestSplitDocumentsIgnoresSeparatorsInsideValues(t *testing.T) {
	// A line of dashes inside a string is not a document separator. Getting
	// this wrong silently truncates whatever follows it.
	body := "apiVersion: v1\nkind: ConfigMap\ndata:\n  note: \"---\"\n  banner: |\n    ---\n    still the same document\n"
	docs := splitDocuments(body)
	if len(docs) != 1 {
		t.Fatalf("want 1 document, got %d: %q", len(docs), docs)
	}
}

func TestSplitDocumentsSeparatesOnItsOwnLine(t *testing.T) {
	docs := splitDocuments("a: 1\n---\nb: 2\n---\nc: 3\n")
	if len(docs) != 3 {
		t.Fatalf("want 3 documents, got %d", len(docs))
	}
	if !strings.Contains(docs[1], "b: 2") {
		t.Errorf("second document is wrong: %q", docs[1])
	}
}

func TestRenderIgnoresObjectsWithNoNameAndUnknownKinds(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "nameless.yaml", "apiVersion: v1\nkind: ConfigMap\n")
	write(t, dir, "unknown.yaml", "apiVersion: acme.io/v1\nkind: Sprocket\nmetadata:\n  name: a\n")
	objs, _, err := render(dir, "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(objs) != 0 {
		t.Fatalf("want nothing usable, got %d", len(objs))
	}
}

func TestClusterManagedObjectsAreNotUnmanagedDrift(t *testing.T) {
	// Kubernetes puts these in every namespace. Reporting them as drift would
	// bury the objects a person actually wrote.
	for _, name := range []string{"kube-root-ca.crt", "default-token-abcde"} {
		if !clusterManaged(name) {
			t.Errorf("%s should be treated as cluster-managed", name)
		}
	}
	if clusterManaged("checkout-settings") {
		t.Error("an ordinary ConfigMap must still count as drift")
	}
}

func TestScanRejectsAPathThatIsNotADirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "deploy.yaml", deployment)
	if _, err := Scan(t.Context(), nil, filepath.Join(dir, "deploy.yaml"), "", false); err == nil {
		t.Error("pointing at a file rather than a directory must fail clearly")
	} else if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("the error should say what is wrong, got %q", err)
	}
}

func TestScanNamesATrailingCharacterOnThePath(t *testing.T) {
	dir := t.TempDir()
	// A path pasted from prose often carries a full stop. The error should
	// say so rather than leaving the person to spot it.
	if _, err := Scan(t.Context(), nil, dir+".", "", false); err == nil {
		t.Error("a path that does not exist must fail")
	} else if !strings.Contains(err.Error(), "trailing character") {
		t.Errorf("the error should mention the trailing character, got %q", err)
	}
}

func TestRenderBuildsAKustomizeOverlay(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "base/deploy.yaml", deployment)
	write(t, dir, "base/kustomization.yaml", "resources:\n  - deploy.yaml\n")
	// The overlay patches the replica count. Comparing the base instead of
	// the rendered overlay would report that patch as drift forever.
	write(t, dir, "overlay/kustomization.yaml",
		"resources:\n  - ../base\npatches:\n  - target:\n      kind: Deployment\n      name: checkout\n    patch: |-\n      - op: replace\n        path: /spec/replicas\n        value: 7\n")

	objs, _, err := render(dir, "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// The base is an input to the overlay, so only the patched result counts.
	if len(objs) != 1 {
		t.Fatalf("want only the overlay's rendered object, got %d", len(objs))
	}
	replicas, _, _ := unstructured.NestedInt64(objs[0].obj.Object, "spec", "replicas")
	if replicas != 7 {
		t.Errorf("want the overlay's patched replica count of 7, got %d", replicas)
	}
}

func TestRenderSkipsABaseThatAnOverlayBuildsOn(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "base/deploy.yaml", deployment)
	write(t, dir, "base/kustomization.yaml", "resources:\n  - deploy.yaml\n")
	write(t, dir, "overlay/kustomization.yaml", "resources:\n  - ../base\n")

	objs, _, err := render(dir, "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("a base consumed by an overlay must not be rendered twice, got %d objects", len(objs))
	}
	if !strings.Contains(objs[0].file, "overlay") {
		t.Errorf("the surviving object should come from the overlay, got %q", objs[0].file)
	}
}

func TestRenderDoesNotWalkIntoAKustomizeRootTwice(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app/deploy.yaml", deployment)
	write(t, dir, "app/kustomization.yaml", "resources:\n  - deploy.yaml\n")

	objs, _, err := render(dir, "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("a file inside a Kustomize root must not also be read as plain YAML, got %d objects", len(objs))
	}
	if !strings.Contains(objs[0].file, "kustomize") {
		t.Errorf("the object should be attributed to the kustomize build, got %q", objs[0].file)
	}
}

func TestRenderReportsAnUnbuildableKustomization(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "kustomization.yaml", "resources:\n  - missing.yaml\n")
	if _, _, err := render(dir, ""); err == nil {
		t.Error("a kustomization that cannot build must fail loudly, not silently render nothing")
	}
}

const chartYAML = `apiVersion: v2
name: shop-frontend
version: 1.0.0
appVersion: "2.1.0"
`

const chartTemplate = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
  namespace: {{ .Release.Namespace }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app: {{ .Release.Name }}
    spec:
      containers:
        - name: web
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
{{- if .Values.ingress.enabled }}
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ .Release.Name }}
  namespace: {{ .Release.Namespace }}
spec:
  rules: []
{{- end }}
`

func writeChart(t *testing.T, dir string, valuesBody string) {
	t.Helper()
	write(t, dir, "Chart.yaml", chartYAML)
	write(t, dir, "templates/deployment.yaml", chartTemplate)
	write(t, dir, "values.yaml", valuesBody)
}

func TestRenderBuildsAHelmChart(t *testing.T) {
	dir := t.TempDir()
	writeChart(t, filepath.Join(dir, "chart"),
		"replicaCount: 3\nimage:\n  repository: nginx\n  tag: 1.27-alpine\ningress:\n  enabled: false\n")

	objs, _, err := render(dir, "shop")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("want the one enabled object, got %d", len(objs))
	}
	o := objs[0]
	// A template is not YAML until Helm has rendered it. These assertions are
	// the difference between comparing intent and comparing gibberish.
	replicas, _, _ := unstructured.NestedInt64(o.obj.Object, "spec", "replicas")
	if replicas != 3 {
		t.Errorf("values should reach the template: want 3 replicas, got %d", replicas)
	}
	if o.obj.GetName() != "shop-frontend" {
		t.Errorf(".Release.Name should be the chart name, got %q", o.obj.GetName())
	}
	if o.obj.GetNamespace() != "shop" {
		t.Errorf(".Release.Namespace should follow the scan namespace, got %q", o.obj.GetNamespace())
	}
	if !strings.Contains(o.file, "helm") {
		t.Errorf("the object should be attributed to the helm build, got %q", o.file)
	}
}

func TestRenderHonoursAConditionalTemplate(t *testing.T) {
	dir := t.TempDir()
	writeChart(t, filepath.Join(dir, "chart"),
		"replicaCount: 1\nimage:\n  repository: nginx\n  tag: latest\ningress:\n  enabled: true\n")

	objs, _, err := render(dir, "shop")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("enabling the ingress should produce a second object, got %d", len(objs))
	}
}

func TestRenderDoesNotWalkIntoAChartsTemplates(t *testing.T) {
	dir := t.TempDir()
	writeChart(t, filepath.Join(dir, "chart"),
		"replicaCount: 1\nimage:\n  repository: nginx\n  tag: latest\ningress:\n  enabled: false\n")

	objs, _, err := render(dir, "shop")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// templates/deployment.yaml is Go template source, not a Kubernetes
	// object. Reading it as plain YAML would produce nonsense.
	for _, o := range objs {
		if !strings.Contains(o.file, "helm") {
			t.Errorf("a chart's files must only arrive through the helm build, got %q", o.file)
		}
	}
	if len(objs) != 1 {
		t.Fatalf("want 1 object, got %d", len(objs))
	}
}

func TestRenderReportsAChartWithMissingDependencies(t *testing.T) {
	dir := t.TempDir()
	chart := filepath.Join(dir, "chart")
	writeChart(t, chart, "replicaCount: 1\nimage:\n  repository: nginx\n  tag: latest\ningress:\n  enabled: false\n")
	write(t, chart, "Chart.yaml", chartYAML+"dependencies:\n  - name: redis\n    version: 1.0.0\n    repository: https://example.invalid\n")

	_, _, err := render(dir, "shop")
	if err == nil {
		t.Fatal("a chart with unvendored dependencies must fail rather than render half of itself")
	}
	if !strings.Contains(err.Error(), "helm dependency build") {
		t.Errorf("the error should say how to fix it, got %q", err)
	}
}

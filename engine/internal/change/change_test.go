package change

import "testing"

// The blast radius is what a reviewer reads before approving, and when the
// author is an agent it must be derived from the plan rather than claimed by
// the author. These tests pin that derivation.

func TestBlastRadiusCountsWhatThePlanTouches(t *testing.T) {
	b, err := blastOf([]Op{
		{Op: "scale", Kind: "deployments", Namespace: "shop", Name: "checkout"},
		{Op: "apply", Kind: "configmaps", Namespace: "shop", Name: "settings"},
		{Op: "delete", Kind: "services", Namespace: "edge", Name: "old"},
	})
	if err != nil {
		t.Fatalf("blastOf: %v", err)
	}
	if b.Objects != 3 {
		t.Errorf("objects: want 3, got %d", b.Objects)
	}
	if b.Deletes != 1 {
		t.Errorf("deletes: want 1, got %d", b.Deletes)
	}
	if len(b.Namespaces) != 2 || b.Namespaces[0] != "edge" || b.Namespaces[1] != "shop" {
		t.Errorf("namespaces should be deduplicated and sorted, got %v", b.Namespaces)
	}
	if len(b.Kinds) != 3 {
		t.Errorf("kinds: want 3 distinct, got %v", b.Kinds)
	}
	if b.HasSecrets {
		t.Error("no Secret is touched by this plan")
	}
}

func TestBlastRadiusFlagsSecrets(t *testing.T) {
	b, err := blastOf([]Op{{Op: "apply", Kind: "secrets", Namespace: "shop", Name: "credentials"}})
	if err != nil {
		t.Fatalf("blastOf: %v", err)
	}
	if !b.HasSecrets {
		t.Error("a plan touching a Secret must be flagged, so a reviewer sees it")
	}
}

func TestBlastRadiusIgnoresEmptyNamespaceForClusterScopedObjects(t *testing.T) {
	b, err := blastOf([]Op{{Op: "apply", Kind: "namespaces", Name: "new-team"}})
	if err != nil {
		t.Fatalf("blastOf: %v", err)
	}
	if len(b.Namespaces) != 0 {
		t.Errorf("a cluster-scoped object contributes no namespace, got %v", b.Namespaces)
	}
	if b.Objects != 1 {
		t.Errorf("objects: want 1, got %d", b.Objects)
	}
}

func TestDesiredScaleSetsReplicas(t *testing.T) {
	live := object(map[string]any{"spec": map[string]any{"replicas": int64(2)}})
	want := int32(5)
	got, err := desired(Op{Op: "scale", Replicas: &want}, live)
	if err != nil {
		t.Fatalf("desired: %v", err)
	}
	if r := got.Object["spec"].(map[string]any)["replicas"]; r != int64(5) {
		t.Errorf("replicas: want 5, got %v", r)
	}
}

func TestDesiredScaleRequiresBothLiveObjectAndReplicas(t *testing.T) {
	five := int32(5)
	if _, err := desired(Op{Op: "scale", Replicas: &five}, nil); err == nil {
		t.Error("scaling an object that does not exist must fail rather than create one")
	}
	if _, err := desired(Op{Op: "scale"}, object(map[string]any{})); err == nil {
		t.Error("scaling without a replica count must fail")
	}
}

func TestDesiredRestartStampsTheTemplate(t *testing.T) {
	got, err := desired(Op{Op: "restart"}, object(map[string]any{}))
	if err != nil {
		t.Fatalf("desired: %v", err)
	}
	spec := got.Object["spec"].(map[string]any)
	tmpl := spec["template"].(map[string]any)["metadata"].(map[string]any)["annotations"].(map[string]any)
	if tmpl["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Error("a restart must stamp the pod template, which is what makes pods roll")
	}
}

func TestDesiredDeleteProducesNoObject(t *testing.T) {
	got, err := desired(Op{Op: "delete"}, object(map[string]any{}))
	if err != nil {
		t.Fatalf("desired: %v", err)
	}
	if got != nil {
		t.Error("a delete has no desired object")
	}
}

func TestDesiredRejectsAnUnknownOperation(t *testing.T) {
	if _, err := desired(Op{Op: "exec"}, object(map[string]any{})); err == nil {
		t.Error("an unknown operation must be rejected rather than silently ignored")
	}
}

func TestDesiredApplyKeepsTheResourceVersionItRead(t *testing.T) {
	live := object(map[string]any{"metadata": map[string]any{"resourceVersion": "4242"}})
	got, err := desired(Op{Op: "apply", Name: "settings", YAML: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n"}, live)
	if err != nil {
		t.Fatalf("desired: %v", err)
	}
	// Carrying the resourceVersion forward is what turns a concurrent edit
	// into a conflict instead of a silent overwrite.
	if got.GetResourceVersion() != "4242" {
		t.Errorf("resourceVersion: want 4242, got %q", got.GetResourceVersion())
	}
}

func TestDesiredApplyRejectsInvalidYAML(t *testing.T) {
	if _, err := desired(Op{Op: "apply", YAML: "\tthis: [is not yaml"}, nil); err == nil {
		t.Error("invalid YAML must fail at propose time, before anything is written")
	}
}

// A plan can name a kind the way the API does rather than the way the
// registry key does. Both reach the same objects, so both must be described
// to the reviewer the same way. The first version of blastOf compared the
// raw string, so "Secret" produced "Secrets: none" and suppressed the
// warning banner on the one kind that most needs it.

func TestBlastRadiusFlagsSecretsByApiKindName(t *testing.T) {
	b, err := blastOf([]Op{{Op: "apply", Kind: "Secret", Namespace: "shop", Name: "credentials"}})
	if err != nil {
		t.Fatalf("blastOf: %v", err)
	}
	if !b.HasSecrets {
		t.Error(`Kind "Secret" reaches the same objects as "secrets" and must be flagged the same way`)
	}
}

func TestBlastRadiusDropsNamespaceOnClusterScopedKinds(t *testing.T) {
	// The apply discards this namespace, so showing it tells the reviewer a
	// cluster-wide delete is confined to one namespace.
	b, err := blastOf([]Op{{Op: "delete", Kind: "clusterrolebindings", Namespace: "sandbox", Name: "cluster-admin"}})
	if err != nil {
		t.Fatalf("blastOf: %v", err)
	}
	if len(b.Namespaces) != 0 {
		t.Errorf("a cluster-scoped op is not confined to a namespace, got %v", b.Namespaces)
	}
}

func TestBlastRadiusRefusesAnUnknownKind(t *testing.T) {
	if _, err := blastOf([]Op{{Op: "apply", Kind: "not-a-kind", Name: "x"}}); err == nil {
		t.Error("a plan whose kind does not resolve cannot have its blast radius described")
	}
}

// The API server takes an object's name from the body of the request. A plan
// entry that says one name while its YAML says another would apply to the
// object the reviewer never saw.

func TestDesiredRefusesYamlThatNamesAnotherObject(t *testing.T) {
	op := Op{Op: "apply", Kind: "secrets", Namespace: "shop", Name: "old-thing",
		YAML: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db-credentials\n  namespace: shop\n"}
	if _, err := desired(op, nil); err == nil {
		t.Error("the write would have landed on db-credentials while the diff showed old-thing")
	}
}

func TestDesiredRefusesGenerateName(t *testing.T) {
	op := Op{Op: "apply", Kind: "configmaps", Namespace: "shop", Name: "settings",
		YAML: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  generateName: settings-\n  namespace: shop\n"}
	if _, err := desired(op, nil); err == nil {
		t.Error("generateName creates an object under a name nobody approved")
	}
}

func TestDesiredAcceptsMatchingYaml(t *testing.T) {
	op := Op{Op: "apply", Kind: "configmaps", Namespace: "shop", Name: "settings",
		YAML: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  namespace: shop\n"}
	if _, err := desired(op, nil); err != nil {
		t.Fatalf("an op whose yaml agrees with it must be accepted: %v", err)
	}
}

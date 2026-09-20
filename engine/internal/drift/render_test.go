package drift

import (
	"os"
	"path/filepath"
	"testing"
)

// A repository is not trusted content. It may be a clone of something the
// person does not control, or a branch someone else pushed.

func TestKustomizeRemoteResourceIsRefused(t *testing.T) {
	for _, ref := range []string{
		"https://example.com/evil.yaml",
		"github.com/evil/repo//base?ref=main",
		"git@example.com:evil/repo.git",
	} {
		dir := t.TempDir()
		body := "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n- " + ref + "\n"
		if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := localRefsOnly(dir); err == nil {
			t.Errorf("a remote resource %q must be refused, not fetched", ref)
		}
	}
}

func TestKustomizeLocalRefsAreAccepted(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.v2")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "kustomization.yaml"),
		[]byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"),
		[]byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n- base.v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := localRefsOnly(dir); err != nil {
		t.Errorf("a directory that merely has a dot in its name is not a hostname: %v", err)
	}
}

func TestSymlinkOutOfTheRepositoryIsRefused(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	chart := filepath.Join(root, "chart")
	if err := os.MkdirAll(chart, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(chart, "loot")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := noEscapingSymlinks(root, chart); err == nil {
		t.Error("a chart that links outside the repository can read a file it was never given")
	}
}

func TestSymlinkInsideTheRepositoryIsAllowed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.yaml"), []byte("kind: ConfigMap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real.yaml"), filepath.Join(root, "link.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := noEscapingSymlinks(root, root); err != nil {
		t.Errorf("a link that stays inside the repository is ordinary: %v", err)
	}
}

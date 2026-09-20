package drift

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const liveService = `apiVersion: v1
kind: Service
metadata:
  name: shop-frontend
  namespace: shop
  uid: 0f6a-not-a-real-uid
  resourceVersion: "4242"
  creationTimestamp: "2026-09-18T12:00:00Z"
  generation: 3
  managedFields:
    - manager: kube-controller-manager
      operation: Update
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: '{"noise":true}'
spec:
  clusterIP: 10.96.0.1
  clusterIPs:
    - 10.96.0.1
  ipFamilies:
    - IPv4
  ports:
    - port: 80
      targetPort: 80
  selector:
    app: shop-frontend
status:
  loadBalancer: {}
`

func TestCleanForGitStripsWhatTheClusterOwns(t *testing.T) {
	out, _, err := cleanForGit([]byte(liveService), false)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	got := string(out)
	// Each of these makes the file a snapshot rather than a declaration, and
	// committing them guarantees a conflict on the next apply.
	for _, unwanted := range []string{
		"status:", "managedFields", "resourceVersion", "uid:", "generation:",
		"creationTimestamp", "last-applied-configuration", "clusterIP", "ipFamilies",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%s should have been stripped:\n%s", unwanted, got)
		}
	}
	for _, wanted := range []string{"kind: Service", "name: shop-frontend", "targetPort: 80", "app: shop-frontend"} {
		if !strings.Contains(got, wanted) {
			t.Errorf("%s is intent and must survive:\n%s", wanted, got)
		}
	}
	// An annotations map emptied by the strip should go too, not be left bare.
	if strings.Contains(got, "annotations:") {
		t.Errorf("an annotations block left empty should be removed:\n%s", got)
	}
}

func TestFileNameIncludesTheNamespace(t *testing.T) {
	if got := fileNameFor(CaptureObject{Kind: "Service", Namespace: "shop", Name: "web"}); got != "shop-service-web.yaml" {
		t.Errorf("got %q", got)
	}
	if got := fileNameFor(CaptureObject{Kind: "Namespace", Name: "shop"}); got != "namespace-shop.yaml" {
		t.Errorf("a cluster-scoped object needs no namespace prefix, got %q", got)
	}
}

func TestCompareURLHandlesTheFormsPeopleActuallyHave(t *testing.T) {
	cases := []struct{ remote, want string }{
		{"git@github.com:jameskomo/clustertrail.git", "https://github.com/jameskomo/clustertrail/compare/b?expand=1"},
		{"https://github.com/jameskomo/clustertrail.git", "https://github.com/jameskomo/clustertrail/compare/b?expand=1"},
		{"ssh://git@github.com/jameskomo/clustertrail.git", "https://github.com/jameskomo/clustertrail/compare/b?expand=1"},
		{"git@gitlab.com:team/infra.git", "https://gitlab.com/team/infra/-/merge_requests/new?merge_request[source_branch]=b"},
		{"/srv/git/bare.git", ""},
	}
	for _, c := range cases {
		if got := compareURL(c.remote, "b"); got != c.want {
			t.Errorf("compareURL(%q):\n got  %q\n want %q", c.remote, got, c.want)
		}
	}
}

// initRepo makes a throwaway repository with one commit, so the capture has
// somewhere to branch from.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "user.name", "Test"},
	} {
		if out, err := git(ctx, dir, args...); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	write(t, dir, "README.md", "# infra\n")
	if _, err := git(ctx, dir, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, dir, "commit", "-q", "-m", "first"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCaptureWritesABranchAndLeavesMainAlone(t *testing.T) {
	repo := initRepo(t)
	ctx := context.Background()

	out, err := CaptureToBranch(ctx, CaptureRequest{
		Repo: repo, Dir: "captured",
		Objects: []CaptureObject{{Kind: "Service", Namespace: "shop", Name: "shop-frontend", YAML: liveService}},
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if len(out.Files) != 1 || out.Files[0] != filepath.Join("captured", "shop-service-shop-frontend.yaml") {
		t.Fatalf("unexpected files: %v", out.Files)
	}
	body, err := os.ReadFile(filepath.Join(repo, out.Files[0]))
	if err != nil {
		t.Fatalf("the file should exist on the branch: %v", err)
	}
	if strings.Contains(string(body), "resourceVersion") {
		t.Error("the committed file must be cleaned")
	}
	if out.Pushed {
		t.Error("nothing should be pushed when Push is false")
	}

	// The branch holds the change; main does not.
	if _, err := git(ctx, repo, "checkout", "-q", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, out.Files[0])); !os.IsNotExist(err) {
		t.Error("main should be untouched by a capture")
	}
}

func TestCaptureRefusesADirtyTree(t *testing.T) {
	repo := initRepo(t)
	write(t, repo, "uncommitted.txt", "work in progress\n")

	_, err := CaptureToBranch(context.Background(), CaptureRequest{
		Repo: repo, Objects: []CaptureObject{{Kind: "Service", Name: "a", YAML: liveService}},
	})
	if err == nil {
		t.Fatal("a dirty tree must be refused, or the branch mixes captured objects with the person's work")
	}
	if !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("the error should say what is wrong, got %q", err)
	}
}

func TestCaptureRefusesSomethingThatIsNotARepository(t *testing.T) {
	_, err := CaptureToBranch(context.Background(), CaptureRequest{
		Repo: t.TempDir(), Objects: []CaptureObject{{Kind: "Service", Name: "a", YAML: liveService}},
	})
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("want a clear error, got %v", err)
	}
}

func TestCaptureRefusesAnEmptyRequest(t *testing.T) {
	if _, err := CaptureToBranch(context.Background(), CaptureRequest{Repo: initRepo(t)}); err == nil {
		t.Error("capturing nothing should be an error, not an empty commit")
	}
}

const liveSecret = `apiVersion: v1
kind: Secret
metadata:
  name: checkout-credentials
  namespace: shop
  resourceVersion: "99"
type: Opaque
data:
  DB_PASSWORD: bm90LWEtcmVhbC1wYXNzd29yZA==
  API_TOKEN: ZGVtby12YWx1ZQ==
`

func TestSecretValuesNeverReachTheRepositoryByDefault(t *testing.T) {
	out, redacted, err := cleanForGit([]byte(liveSecret), false)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	got := string(out)
	// Committing base64 is not encryption. A repository is the wrong home for
	// a credential, and making it one click easy is how breaches happen.
	for _, value := range []string{"bm90LWEtcmVhbC1wYXNzd29yZA==", "ZGVtby12YWx1ZQ=="} {
		if strings.Contains(got, value) {
			t.Fatalf("a Secret value reached the file:\n%s", got)
		}
	}
	// The keys stay, because the shape is what a reviewer needs to see.
	for _, key := range []string{"DB_PASSWORD", "API_TOKEN"} {
		if !strings.Contains(got, key) {
			t.Errorf("the key %s should survive so the declaration is reviewable", key)
		}
	}
	if !strings.Contains(got, secretPlaceholder) {
		t.Error("each value should carry a placeholder")
	}
	if !strings.Contains(got, "not a secret store") {
		t.Error("the file should explain why the values are missing and what to do instead")
	}
	if len(redacted) != 2 {
		t.Errorf("both keys should be reported as redacted, got %v", redacted)
	}
}

func TestSecretValuesCanBeIncludedDeliberately(t *testing.T) {
	out, redacted, err := cleanForGit([]byte(liveSecret), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "bm90LWEtcmVhbC1wYXNzd29yZA==") {
		t.Error("an explicit opt-in should write the values")
	}
	if len(redacted) != 0 {
		t.Errorf("nothing was redacted, got %v", redacted)
	}
}

func TestCaptureReportsWhatItRedacted(t *testing.T) {
	repo := initRepo(t)
	out, err := CaptureToBranch(context.Background(), CaptureRequest{
		Repo: repo, Dir: "captured",
		Objects: []CaptureObject{{Kind: "Secret", Namespace: "shop", Name: "checkout-credentials", YAML: liveSecret}},
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if len(out.Redacted) != 2 {
		t.Fatalf("want both keys reported, got %v", out.Redacted)
	}
	if !strings.Contains(out.Message, "placeholder") {
		t.Errorf("the person must be told the values were not captured, got %q", out.Message)
	}
	body, err := os.ReadFile(filepath.Join(repo, out.Files[0]))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "bm90LWEtcmVhbC1wYXNzd29yZA==") {
		t.Fatal("the committed file must not contain the value")
	}
	// The commit message is where a reviewer looks first.
	msg, err := git(context.Background(), repo, "log", "-1", "--pretty=%B")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Secret values were NOT captured") {
		t.Errorf("the commit message should say so, got:\n%s", msg)
	}
}

// The values below arrive over a socket and are used to build file paths and
// git arguments. They are not trusted on the strength of a convention.

func TestCaptureCannotWriteOutsideTheRepository(t *testing.T) {
	repo := initRepo(t)
	outside := filepath.Join(filepath.Dir(repo), "escaped")

	_, err := CaptureToBranch(context.Background(), CaptureRequest{
		Repo: repo, Dir: "../../escaped",
		Objects: []CaptureObject{{Kind: "Service", Namespace: "shop", Name: "web", YAML: liveService}},
	})
	if err == nil {
		t.Fatal("a directory outside the repository must be refused, not quietly rewritten")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("a directory escaped the repository at %s", outside)
	}
}

func TestCaptureWillNotOverwriteAFileTheRepositoryAlreadyHas(t *testing.T) {
	repo := initRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "captured"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(repo, "captured", "shop-service-web.yaml")
	declared := []byte("apiVersion: v1\nkind: Service\nmetadata:\n  name: web\n  namespace: shop\nspec:\n  ports:\n  - port: 8080\n")
	if err := os.WriteFile(existing, declared, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git(context.Background(), repo, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(context.Background(), repo, "commit", "-m", "declare web"); err != nil {
		t.Fatal(err)
	}

	if _, err := CaptureToBranch(context.Background(), CaptureRequest{
		Repo: repo, Dir: "captured",
		Objects: []CaptureObject{{Kind: "Service", Namespace: "shop", Name: "web", YAML: liveService}},
	}); err == nil {
		t.Fatal("capture must not replace a file that already declares this object")
	}
	after, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(declared) {
		t.Error("the repository's own declaration was overwritten by a snapshot of the cluster")
	}
}

func TestSafeDirRefusesAnythingOutsideTheRepository(t *testing.T) {
	// Rewriting a path the caller did not ask for is the wrong failure: the
	// caller believes they wrote one place and the file lands in another.
	for _, bad := range []string{
		"../../etc",
		"/etc/passwd",
		"a/../../../b",
		".git/hooks",
		".github/workflows",
		"captured/../../..",
	} {
		if got, err := safeDir(bad); err == nil {
			t.Errorf("safeDir(%q) should be refused, got %q", bad, got)
		}
	}
}

func TestSafeDirAcceptsOrdinaryRelativeDirectories(t *testing.T) {
	cases := map[string]string{
		"captured":       "captured",
		"":               "",
		"  captured  ":   "captured",
		"./captured/./x": "captured/x",
		"a/b/c":          "a/b/c",
	}
	for in, want := range cases {
		got, err := safeDir(in)
		if err != nil {
			t.Errorf("safeDir(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("safeDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoteNamesThatGitWouldReadAsOptionsAreRefused(t *testing.T) {
	// A remote name comes out of the repository's own .git/config, which is
	// a file in a directory somebody handed us.
	for _, bad := range []string{
		"--receive-pack=touch /tmp/x",
		"-x",
		"--mirror",
		"..",
	} {
		if _, err := safeGitValue("remote", bad); err == nil {
			t.Errorf("remote %q should be refused", bad)
		}
	}
	if got, err := safeGitValue("remote", "origin"); err != nil || got != "origin" {
		t.Errorf(`safeGitValue("remote", "origin") = %q, %v`, got, err)
	}
}

func TestBranchNamesThatGitWouldReadAsOptionsAreRefused(t *testing.T) {
	// A leading dash makes the value a flag, and git has flags that run
	// commands. This is validated rather than escaped.
	for _, bad := range []string{
		"--upload-pack=touch /tmp/x",
		"-x",
		"has space",
		"semi;colon",
		"back`tick`",
		"$(command)",
		"dotdot/../escape",
		"ends.lock",
	} {
		if _, err := safeBranch(bad); err == nil {
			t.Errorf("branch %q should be refused", bad)
		}
	}
	for _, good := range []string{"clustertrail/capture-20260919", "main", "feature/x_1.2"} {
		if _, err := safeBranch(good); err != nil {
			t.Errorf("branch %q should be allowed: %v", good, err)
		}
	}
	generated, err := safeBranch("")
	if err != nil || !strings.HasPrefix(generated, "clustertrail/") {
		t.Errorf("an empty branch should generate one, got %q %v", generated, err)
	}
}

func TestCaptureRefusesAHostileBranchName(t *testing.T) {
	repo := initRepo(t)
	_, err := CaptureToBranch(context.Background(), CaptureRequest{
		Repo: repo, Branch: "--upload-pack=touch /tmp/pwned",
		Objects: []CaptureObject{{Kind: "Service", Name: "web", YAML: liveService}},
	})
	if err == nil {
		t.Fatal("a branch name that git would read as an option must be refused")
	}
	if !strings.Contains(err.Error(), "not usable") {
		t.Errorf("the error should explain, got %q", err)
	}
}

func TestFileNamesCannotEscapeOrSurprise(t *testing.T) {
	got := fileNameFor(CaptureObject{Kind: "Secret", Namespace: "../../etc", Name: "../../passwd"})
	if strings.Contains(got, "/") || strings.Contains(got, "..") {
		t.Errorf("a name must not build a path: %q", got)
	}
	if got := fileNameFor(CaptureObject{Kind: "Secret", Name: "..."}); strings.Contains(got, "..") {
		t.Errorf("dots should be trimmed: %q", got)
	}
	// A cluster-scoped object has no namespace, and inventing one would put a
	// stray word into every file name.
	if got := safeSegment(""); got != "" {
		t.Errorf("an empty segment must stay empty, got %q", got)
	}
	if got := fileNameFor(CaptureObject{Kind: "!!!", Name: "???"}); got != "object-unnamed.yaml" {
		t.Errorf("unusable names need a fallback, got %q", got)
	}
}

// Pushing a branch to a remote with no branches does not publish a branch,
// it publishes the repository: every commit reachable from this one, files
// deleted long ago included, because deleting a file does not remove its
// blob. This happened to this project.
func TestCaptureRefusesToPushToAnEmptyRemote(t *testing.T) {
	repo := initRepo(t)
	bare := t.TempDir()
	if _, err := git(context.Background(), bare, "init", "--bare", "--initial-branch=main", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := git(context.Background(), repo, "remote", "add", "origin", bare); err != nil {
		t.Fatal(err)
	}

	out, err := CaptureToBranch(context.Background(), CaptureRequest{
		Repo: repo, Dir: "captured", Push: true,
		Objects: []CaptureObject{{Kind: "Service", Namespace: "shop", Name: "web", YAML: liveService}},
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if out.Pushed {
		t.Error("a push to an empty remote publishes the whole repository and must be refused")
	}
	if !strings.Contains(out.Message, "entire history") {
		t.Errorf("the refusal should explain why, got: %s", out.Message)
	}
	remote, err := git(context.Background(), repo, "ls-remote", "--heads", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(remote) != "" {
		t.Errorf("nothing should have reached the remote, got: %s", remote)
	}
}

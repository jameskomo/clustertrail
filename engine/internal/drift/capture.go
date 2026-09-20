package drift

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// Capture writes drifted objects back into a repository as a branch, and
// returns the URL that opens a pull request for it.
//
// This is the half of drift that goes the other way: the cluster is right and
// Git is behind. Everything here runs git in the repository the person chose;
// nothing touches a cluster, and nothing is pushed without a remote that
// already exists and credentials the person already has.
type Capture struct {
	Repo       string   `json:"repo"`
	Branch     string   `json:"branch"`
	Files      []string `json:"files"`
	Pushed     bool     `json:"pushed"`
	CompareURL string   `json:"compareUrl,omitempty"`
	Message    string   `json:"message"`
	Redacted   []string `json:"redacted,omitempty"`
}

// CaptureRequest names the objects to write and where to put them.
type CaptureRequest struct {
	Repo    string // the working tree to write into
	Dir     string // where inside it, relative; defaults to the repository root
	Branch  string // defaults to a generated name
	Push    bool   // push and offer a compare URL
	Objects []CaptureObject

	// IncludeSecretValues writes Secret data into the repository. It is off by
	// default and should stay off: committing base64 is not encryption, and a
	// repository is the wrong place for a credential.
	IncludeSecretValues bool
}

// CaptureObject is one object's YAML and the file name to give it.
type CaptureObject struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	YAML      string `json:"yaml"`
}

// secretPlaceholder replaces the value of every key in a Secret. The keys stay,
// because the shape of a Secret is part of the declaration and a reviewer needs
// to see which keys exist; the values do not, because a git repository is not a
// secret store and base64 is not encryption.
const secretPlaceholder = "REPLACE_ME"

// stripSecretData empties a Secret's values and leaves a note saying what to do
// instead. Committing a live credential because a tool made it one click easy
// is the kind of mistake that ends up in a breach report.
func stripSecretData(u *unstructured.Unstructured) (keys []string) {
	for _, field := range []string{"data", "stringData"} {
		raw, present := u.Object[field]
		if !present {
			continue
		}
		block, isMap := raw.(map[string]any)
		if !isMap {
			// A data field that is not a map is not a shape we understand.
			// Drop it: an unrecognised shape must not carry values into Git.
			unstructured.RemoveNestedField(u.Object, field)
			keys = append(keys, field)
			continue
		}
		if len(block) == 0 {
			continue
		}
		replaced := make(map[string]any, len(block))
		for k := range block {
			keys = append(keys, k)
			replaced[k] = secretPlaceholder
		}
		unstructured.RemoveNestedField(u.Object, field)
		// stringData keeps the placeholder readable rather than base64.
		_ = unstructured.SetNestedMap(u.Object, replaced, "stringData")
		break
	}
	sort.Strings(keys)
	return keys
}

// cleanForGit strips the fields the API server owns. A file committed with a
// resourceVersion or a status block is not a declaration of intent, it is a
// snapshot, and it will conflict with the next apply.
func cleanForGit(body []byte, includeSecretValues bool) ([]byte, []string, error) {
	u := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(body, &u.Object); err != nil {
		return nil, nil, err
	}
	var redacted []string
	if strings.EqualFold(u.GetKind(), "Secret") && !includeSecretValues {
		redacted = stripSecretData(u)
	}
	for _, path := range [][]string{
		{"status"},
		{"metadata", "managedFields"},
		{"metadata", "resourceVersion"},
		{"metadata", "uid"},
		{"metadata", "generation"},
		{"metadata", "creationTimestamp"},
		{"metadata", "selfLink"},
		{"metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration"},
		{"metadata", "annotations", "deployment.kubernetes.io/revision"},
	} {
		unstructured.RemoveNestedField(u.Object, path...)
	}
	if ann, ok, _ := unstructured.NestedMap(u.Object, "metadata", "annotations"); ok && len(ann) == 0 {
		unstructured.RemoveNestedField(u.Object, "metadata", "annotations")
	}
	// A Service's assigned addresses belong to the cluster, not to the repo.
	if u.GetKind() == "Service" {
		for _, f := range []string{"clusterIP", "clusterIPs", "ipFamilies", "ipFamilyPolicy", "internalTrafficPolicy"} {
			unstructured.RemoveNestedField(u.Object, "spec", f)
		}
	}
	out, err := yaml.Marshal(u.Object)
	if err != nil {
		return nil, nil, err
	}
	if len(redacted) > 0 {
		note := "# ClusterTrail removed this Secret's values. The keys are kept so the\n" +
			"# shape is reviewable; the values were not written, because a repository\n" +
			"# is not a secret store and base64 is not encryption.\n" +
			"#\n" +
			"# Fill them from your secret manager, or replace this file with a\n" +
			"# SealedSecret, an ExternalSecret, or a SOPS-encrypted file.\n"
		out = append([]byte(note), out...)
	}
	return out, redacted, nil
}

func fileNameFor(o CaptureObject) string {
	kind := orFallback(safeSegment(strings.ToLower(o.Kind)), "object")
	name := orFallback(safeSegment(o.Name), "unnamed")
	if ns := safeSegment(o.Namespace); ns != "" {
		return fmt.Sprintf("%s-%s-%s.yaml", ns, kind, name)
	}
	return fmt.Sprintf("%s-%s.yaml", kind, name)
}

func orFallback(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// safeSegment reduces a name to characters that cannot escape a directory or
// surprise a shell. Kubernetes names are already restricted, but these values
// arrive over a socket and are used to build paths, so they are not trusted
// on the strength of a convention.
func safeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	// An empty result stays empty: a cluster-scoped object has no namespace,
	// and inventing one would put a stray word in every file name.
	return strings.Trim(b.String(), ".-")
}

// safeDir confines a caller-supplied folder to inside the repository.
//
// It refuses rather than rewrites. An earlier version rooted the path at "/"
// and cleaned it, which turned "../../etc" into "etc": the caller did not get
// an error, they got a file in a directory they never named. For a function
// whose job is confinement that is the wrong failure, and it is how a request
// aimed outside the repository ended up writing into .github/workflows.
func safeDir(dir string) (string, error) {
	d := strings.TrimSpace(dir)
	if d == "" {
		return "", nil
	}
	if filepath.IsAbs(d) || strings.HasPrefix(d, "/") || strings.HasPrefix(d, `\\`) {
		return "", fmt.Errorf("capture directory %q must be relative to the repository", dir)
	}
	cleaned := filepath.Clean(filepath.FromSlash(d))
	for _, seg := range strings.Split(cleaned, string(os.PathSeparator)) {
		switch {
		case seg == "..":
			return "", fmt.Errorf("capture directory %q must stay inside the repository", dir)
		case strings.HasPrefix(seg, "."):
			// .git holds the repository's own state and .github holds code
			// that CI executes. Neither is somewhere a capture belongs.
			return "", fmt.Errorf("capture directory %q must not write into a dot-directory", dir)
		}
	}
	return cleaned, nil
}

// insideRepo resolves symlinks and reports whether the result is still under
// repo. Cleaning a path is lexical: it says nothing about a symlink committed
// in the repository that points somewhere else entirely.
func insideRepo(repo, path string) error {
	base, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// The file itself need not exist yet; its directory must.
		parent, perr := filepath.EvalSymlinks(filepath.Dir(path))
		if perr != nil {
			return perr
		}
		resolved = filepath.Join(parent, filepath.Base(path))
	}
	if resolved != base && !strings.HasPrefix(resolved, base+string(os.PathSeparator)) {
		return fmt.Errorf("%s resolves outside the repository", path)
	}
	return nil
}

// safeBranch rejects anything git would read as an option or a path.
//
// A branch name arrives over the socket and is passed to git as an argument.
// A value starting with a dash becomes a flag, and git has flags that run
// commands, so this is validated rather than escaped.
var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,100}$`)

func safeBranch(branch string) (string, error) {
	b := strings.TrimSpace(branch)
	if b == "" {
		return "clustertrail/capture-" + time.Now().UTC().Format("20060102-1504"), nil
	}
	if !branchPattern.MatchString(b) || strings.Contains(b, "..") || strings.HasSuffix(b, ".lock") {
		return "", fmt.Errorf("branch name %q is not usable: letters, digits, dot, dash, underscore and slash only, starting with a letter or digit", branch)
	}
	return b, nil
}

// CaptureToBranch writes the objects, commits them on a new branch, and
// optionally pushes and builds a compare URL.
func CaptureToBranch(ctx context.Context, req CaptureRequest) (*Capture, error) {
	if len(req.Objects) == 0 {
		return nil, fmt.Errorf("nothing to capture")
	}
	repo := strings.TrimSpace(req.Repo)
	if err := isGitRepo(ctx, repo); err != nil {
		return nil, err
	}
	if dirty, err := hasUncommittedChanges(ctx, repo); err != nil {
		return nil, err
	} else if dirty {
		return nil, fmt.Errorf("%s has uncommitted changes; commit or stash them first so the branch holds only the captured objects", repo)
	}

	branch, err := safeBranch(req.Branch)
	if err != nil {
		return nil, err
	}
	dir, err := safeDir(req.Dir)
	if err != nil {
		return nil, err
	}
	start, err := currentBranch(ctx, repo)
	if err != nil {
		return nil, err
	}
	if _, err := git(ctx, repo, "checkout", "-b", branch); err != nil {
		return nil, fmt.Errorf("create branch %s: %w", branch, err)
	}
	// Any failure from here leaves the person back where they started.
	restore := func() { _, _ = git(ctx, repo, "checkout", start) }

	target := filepath.Join(repo, dir)
	if err := os.MkdirAll(target, 0o755); err != nil {
		restore()
		return nil, err
	}
	if err := insideRepo(repo, target); err != nil {
		restore()
		return nil, err
	}

	var written []string
	var redacted []string
	for _, o := range req.Objects {
		body, stripped, err := cleanForGit([]byte(o.YAML), req.IncludeSecretValues)
		if err != nil {
			restore()
			return nil, fmt.Errorf("%s/%s: %w", o.Kind, o.Name, err)
		}
		for _, k := range stripped {
			redacted = append(redacted, o.Name+"."+k)
		}
		rel := filepath.Join(dir, fileNameFor(o))
		abs := filepath.Join(repo, rel)
		if err := insideRepo(repo, abs); err != nil {
			restore()
			return nil, err
		}
		// O_EXCL rather than a plain write. A file already at this path is
		// somebody's declared intent, and a tool that exists to protect that
		// intent must not quietly replace it with a snapshot of the cluster.
		f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			restore()
			if os.IsExist(err) {
				return nil, fmt.Errorf("%s already exists; capture will not overwrite a file the repository already has", rel)
			}
			return nil, err
		}
		_, werr := f.Write(body)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			restore()
			return nil, werr
		}
		written = append(written, rel)
	}

	if _, err := git(ctx, repo, append([]string{"add", "--"}, written...)...); err != nil {
		restore()
		return nil, err
	}
	subject := fmt.Sprintf("Capture %d object%s running without a file", len(written), plural(len(written)))
	body := "Captured from the live cluster by ClusterTrail.\n\nThese objects were running with nothing in this repository describing them.\nServer-owned fields have been stripped, so each file is a declaration of\nintent rather than a snapshot. Review before merging.\n"
	if len(redacted) > 0 {
		body += fmt.Sprintf("\nSecret values were NOT captured. %d key%s carry a placeholder and must be\nfilled from a secret manager before this is applied:\n  %s\n",
			len(redacted), plural(len(redacted)), strings.Join(redacted, "\n  "))
	}
	if _, err := git(ctx, repo, "commit", "-m", subject, "-m", body); err != nil {
		restore()
		return nil, fmt.Errorf("commit: %w", err)
	}

	out := &Capture{Repo: repo, Branch: branch, Files: written, Redacted: redacted,
		Message: fmt.Sprintf("Committed %d file%s on %s.", len(written), plural(len(written)), branch)}
	if len(redacted) > 0 {
		out.Message += fmt.Sprintf(" %d Secret value%s replaced with a placeholder; fill them from your secret manager.",
			len(redacted), plural(len(redacted)))
	}

	if req.Push {
		remote, err := defaultRemote(ctx, repo)
		if err != nil {
			out.Message += " No remote to push to, so the branch is local."
			return out, nil
		}
		// A push to a remote that has no branches yet does not publish a
		// branch: it publishes the repository. Every commit reachable from
		// this one goes with it, including files deleted long ago, because
		// deleting a file does not remove it from history. That is not what
		// anyone means by "capture this drift and open a pull request", so
		// it is refused and the person is told to publish deliberately.
		if empty, err := remoteHasNoBranches(ctx, repo, remote); err == nil && empty {
			out.Message += fmt.Sprintf(" %s has no branches yet, so pushing would publish this repository's entire history rather than just this branch."+
				" The branch is committed locally; push it yourself once %s holds the history you mean to share.", remote, remote)
			return out, nil
		}
		if _, err := git(ctx, repo, "push", "--set-upstream", remote, branch); err != nil {
			out.Message += fmt.Sprintf(" Pushing to %s failed: %v. The branch is committed locally.", remote, err)
			return out, nil
		}
		out.Pushed = true
		out.Message = fmt.Sprintf("Pushed %s to %s.", branch, remote)
		if url, err := remoteURL(ctx, repo, remote); err == nil {
			if compare := compareURL(url, branch); compare != "" {
				out.CompareURL = compare
				out.Message += " Open the link to raise the pull request."
			}
		}
	}
	return out, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func git(ctx context.Context, repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// The repository is a directory someone pointed us at, and .git/config
	// and .git/hooks are executable surfaces inside it. Neither should run
	// because a capture wrote a file.
	hardened := append([]string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "core.pager=cat",
		"-c", "core.askPass=",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", hardened...)
	cmd.Dir = repo
	// Never let git stop for a credential prompt inside a desktop app.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func isGitRepo(ctx context.Context, repo string) error {
	if _, err := os.Stat(repo); err != nil {
		return fmt.Errorf("repository path: %w", err)
	}
	if _, err := git(ctx, repo, "rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("%s is not a git repository", repo)
	}
	return nil
}

func hasUncommittedChanges(ctx context.Context, repo string) (bool, error) {
	out, err := git(ctx, repo, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func currentBranch(ctx context.Context, repo string) (string, error) {
	return git(ctx, repo, "rev-parse", "--abbrev-ref", "HEAD")
}

// gitValuePattern is applied to every value that reaches git as an argument.
//
// safeBranch already did this for the branch name. The same reasoning applies
// to a remote name, which comes from the repository's own .git/config: a
// value beginning with a dash is read as a flag, and git has flags that run
// commands. `--receive-pack=...` on a push is the one that matters.
var gitValuePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,100}$`)

func safeGitValue(what, v string) (string, error) {
	t := strings.TrimSpace(v)
	if !gitValuePattern.MatchString(t) || strings.Contains(t, "..") {
		return "", fmt.Errorf("%s %q is not usable: letters, digits, dot, dash, underscore and slash only, starting with a letter or digit", what, v)
	}
	return t, nil
}

func defaultRemote(ctx context.Context, repo string) (string, error) {
	out, err := git(ctx, repo, "remote")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no remote configured")
	}
	remotes := strings.Fields(out)
	pick := remotes[0]
	for _, r := range remotes {
		if r == "origin" {
			pick = r
			break
		}
	}
	return safeGitValue("remote", pick)
}

// remoteHasNoBranches reports whether the remote is still empty.
func remoteHasNoBranches(ctx context.Context, repo, remote string) (bool, error) {
	out, err := git(ctx, repo, "ls-remote", "--heads", "--", remote)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

func remoteURL(ctx context.Context, repo, remote string) (string, error) {
	return git(ctx, repo, "remote", "get-url", remote)
}

// compareURL turns a git remote into the page that opens a pull request.
// Both SSH and HTTPS forms are handled for the two hosts most people use.
func compareURL(remote, branch string) string {
	r := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	switch {
	case strings.HasPrefix(r, "git@"):
		// git@github.com:owner/repo
		parts := strings.SplitN(strings.TrimPrefix(r, "git@"), ":", 2)
		if len(parts) != 2 {
			return ""
		}
		r = "https://" + parts[0] + "/" + parts[1]
	case strings.HasPrefix(r, "ssh://git@"):
		r = "https://" + strings.TrimPrefix(r, "ssh://git@")
	case !strings.HasPrefix(r, "http"):
		return ""
	}
	switch {
	case strings.Contains(r, "github.com"):
		return fmt.Sprintf("%s/compare/%s?expand=1", r, branch)
	case strings.Contains(r, "gitlab"):
		return fmt.Sprintf("%s/-/merge_requests/new?merge_request[source_branch]=%s", r, branch)
	}
	return ""
}

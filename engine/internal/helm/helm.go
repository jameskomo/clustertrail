// Package helm reads Helm releases straight from the objects Helm stores in
// the cluster. Helm keeps each revision in a Secret (or ConfigMap) whose
// payload is base64(gzip(json)), so browsing releases needs no Helm binary,
// no chart repository and no extra dependency.
package helm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
)

// Release is one revision of one Helm release.
type Release struct {
	Name        string    `json:"name"`
	Namespace   string    `json:"namespace"`
	Revision    int       `json:"revision"`
	Status      string    `json:"status"`
	Chart       string    `json:"chart"`
	AppVersion  string    `json:"appVersion"`
	Updated     time.Time `json:"updated"`
	Description string    `json:"description,omitempty"`
}

// Detail adds the payloads a person wants to read.
type Detail struct {
	Release
	Values   string    `json:"values"`
	Manifest string    `json:"manifest"`
	Notes    string    `json:"notes"`
	History  []Release `json:"history"`
}

type storedRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Info      struct {
		LastDeployed time.Time `json:"last_deployed"`
		Status       string    `json:"status"`
		Notes        string    `json:"notes"`
		Description  string    `json:"description"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
	Config   map[string]any `json:"config"`
	Manifest string         `json:"manifest"`
}

// decode unwraps Helm's storage encoding: the Secret value is base64 (done by
// the API machinery), then Helm's own base64, then gzip, then JSON.
// maxRelease bounds what a single stored release may expand to. Anyone who
// can create a Secret in a namespace can store a gzip bomb there: a 1 MiB
// value, which is all the API server allows, decompresses to something near a
// gigabyte. Reading it without a bound takes the whole engine down, and every
// cluster session, port-forward and terminal with it.
const maxRelease = 64 << 20

func decode(data []byte) (*storedRelease, error) {
	if len(data) > maxRelease {
		return nil, fmt.Errorf("release record is %d bytes, larger than the %d ClusterTrail will read", len(data), maxRelease)
	}
	raw := data
	if decoded, err := base64.StdEncoding.DecodeString(string(data)); err == nil {
		raw = decoded
	}
	if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		// One byte past the limit is how an over-long stream is told apart
		// from one that merely fills it.
		limited, err := io.ReadAll(io.LimitReader(zr, maxRelease+1))
		if err != nil {
			return nil, err
		}
		if len(limited) > maxRelease {
			return nil, fmt.Errorf("release expands past %d bytes; refusing to read it", maxRelease)
		}
		raw = limited
	}
	var sr storedRelease
	if err := json.Unmarshal(raw, &sr); err != nil {
		return nil, fmt.Errorf("parse release: %w", err)
	}
	return &sr, nil
}

func toRelease(sr *storedRelease) Release {
	return Release{
		Name: sr.Name, Namespace: sr.Namespace, Revision: sr.Version,
		Status: sr.Info.Status, Chart: sr.Chart.Metadata.Name + "-" + sr.Chart.Metadata.Version,
		AppVersion: sr.Chart.Metadata.AppVersion, Updated: sr.Info.LastDeployed, Description: sr.Info.Description,
	}
}

func revisions(ctx context.Context, sess *cluster.Session, namespace string) ([]*storedRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out := []*storedRelease{}
	secrets, err := sess.Client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err == nil {
		for i := range secrets.Items {
			if sr, err := decode(secrets.Items[i].Data["release"]); err == nil {
				out = append(out, sr)
			}
		}
	}
	// Helm 2-style and explicitly configured ConfigMap storage.
	cms, err2 := sess.Client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err2 == nil {
		for i := range cms.Items {
			if sr, err := decode([]byte(cms.Items[i].Data["release"])); err == nil {
				out = append(out, sr)
			}
		}
	}
	if err != nil && err2 != nil {
		return nil, err
	}
	return out, nil
}

// List returns the newest revision of every release, newest first.
func List(ctx context.Context, sess *cluster.Session, namespace string) ([]Release, error) {
	all, err := revisions(ctx, sess, namespace)
	if err != nil {
		return nil, err
	}
	latest := map[string]*storedRelease{}
	for _, sr := range all {
		key := sr.Namespace + "/" + sr.Name
		if cur, ok := latest[key]; !ok || sr.Version > cur.Version {
			latest[key] = sr
		}
	}
	out := make([]Release, 0, len(latest))
	for _, sr := range latest {
		out = append(out, toRelease(sr))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// Get returns one release with its values, manifest, notes and history.
func Get(ctx context.Context, sess *cluster.Session, namespace, name string, revision int) (*Detail, error) {
	all, err := revisions(ctx, sess, namespace)
	if err != nil {
		return nil, err
	}
	var hist []Release
	var want *storedRelease
	for _, sr := range all {
		if sr.Name != name {
			continue
		}
		hist = append(hist, toRelease(sr))
		if (revision == 0 && (want == nil || sr.Version > want.Version)) || sr.Version == revision {
			want = sr
		}
	}
	if want == nil {
		return nil, fmt.Errorf("release %s not found in %s", name, namespace)
	}
	sort.Slice(hist, func(i, j int) bool { return hist[i].Revision > hist[j].Revision })
	values := "{}\n"
	if len(want.Config) > 0 {
		if b, err := json.MarshalIndent(want.Config, "", "  "); err == nil {
			values = string(b)
		}
	}
	return &Detail{Release: toRelease(want), Values: values, Manifest: want.Manifest, Notes: want.Info.Notes, History: hist}, nil
}

// RollbackPlan describes what rolling a release back would do, without doing
// any of it.
//
// ClusterTrail does not reimplement Helm's release lifecycle. A rollback here means
// applying the manifest of an earlier revision through the same change gate as
// everything else, so the diff is reviewed and the decision is recorded. Helm
// itself is not told about it, which is stated plainly in the plan, because a
// half-written release history is worse than none.
type RollbackPlan struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	From      int      `json:"from"`
	To        int      `json:"to"`
	Objects   []Object `json:"objects"`
	Warning   string   `json:"warning"`
}

// Object is one manifest document from a release revision.
type Object struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	YAML      string `json:"yaml"`
}

// PlanRollback reads the target revision and returns the objects that would be
// applied. It writes nothing.
func PlanRollback(ctx context.Context, sess *cluster.Session, namespace, name string, to int) (*RollbackPlan, error) {
	if to <= 0 {
		return nil, fmt.Errorf("choose the revision to roll back to")
	}
	current, err := Get(ctx, sess, namespace, name, 0)
	if err != nil {
		return nil, err
	}
	if to == current.Revision {
		return nil, fmt.Errorf("revision %d is already deployed", to)
	}
	target, err := Get(ctx, sess, namespace, name, to)
	if err != nil {
		return nil, fmt.Errorf("revision %d: %w", to, err)
	}

	objs, err := splitManifest(target.Manifest, namespace)
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("revision %d has no manifest to apply", to)
	}
	return &RollbackPlan{
		Name: name, Namespace: namespace, From: current.Revision, To: to, Objects: objs,
		Warning: "ClusterTrail applies the objects from revision " + strconv.Itoa(to) +
			" through the review gate. Helm's own release history is not updated, so `helm list` will still report revision " +
			strconv.Itoa(current.Revision) + ". Run `helm rollback` if you need Helm's records to match.",
	}, nil
}

// splitManifest turns a rendered Helm manifest into individual objects.
func splitManifest(manifest, defaultNamespace string) ([]Object, error) {
	var out []Object
	for _, doc := range strings.Split(manifest, "\n---") {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" {
			continue
		}
		var head struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(doc), &head); err != nil || head.Kind == "" || head.Metadata.Name == "" {
			continue
		}
		ns := head.Metadata.Namespace
		if ns == "" {
			ns = defaultNamespace
		}
		out = append(out, Object{Kind: head.Kind, Namespace: ns, Name: head.Metadata.Name, YAML: trimmed + "\n"})
	}
	return out, nil
}

// Package explain asks a model what is wrong with one object.
//
// It is read-only by construction: the model is handed evidence and returns
// prose. It has no tools and no way to reach the cluster. Anything it suggests
// doing is text a person then chooses to act on through the Change gate.
package explain

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/kinds"
	"github.com/jameskomo/clustertrail/engine/internal/watch"
)

const systemPrompt = `You are helping a platform engineer read one Kubernetes object during an incident.

You are given the object, its recent events, and recent log lines. Answer in this shape:

1. One sentence saying what is wrong, or that nothing looks wrong.
2. The evidence you used, quoting the specific event reason or log line.
3. The most likely cause.
4. What you would check or change next, as concrete kubectl-level steps.

Rules: rely only on the evidence given, and say plainly when it is not enough to tell.
Never claim you have done anything; you cannot act on the cluster. Keep it under 250 words,
plain sentences, no headings.`

// Keys are held in memory for the life of the engine process. They are never
// written to the local store or the audit log.
var (
	mu   sync.RWMutex
	keys = map[string]string{}
)

// SetKey records the API key for a provider ("anthropic").
func SetKey(provider, key string) {
	mu.Lock()
	defer mu.Unlock()
	if key == "" {
		delete(keys, provider)
		return
	}
	keys[provider] = key
}

// HasKey reports whether a key is available, from the UI or the environment.
func HasKey(provider string) bool {
	mu.RLock()
	k := keys[provider]
	mu.RUnlock()
	return k != "" || os.Getenv("ANTHROPIC_API_KEY") != ""
}

// KeyFromEnv reports that the only key available came from the environment
// rather than from this session. The pane is then armed with a credential
// nobody typed into ClusterTrail, which a person should be told rather than
// left to discover.
func KeyFromEnv(provider string) bool {
	mu.RLock()
	k := keys[provider]
	mu.RUnlock()
	return k == "" && os.Getenv("ANTHROPIC_API_KEY") != ""
}

func keyFor(provider string) string {
	mu.RLock()
	k := keys[provider]
	mu.RUnlock()
	if k != "" {
		return k
	}
	return os.Getenv("ANTHROPIC_API_KEY")
}

// Evidence is what the model is shown. It is returned to the UI as well, so a
// person can see exactly what was sent.
type Evidence struct {
	Object string   `json:"object"`
	Events []string `json:"events"`
	Logs   []string `json:"logs"`
}

// Gather collects the object, its events and recent logs, bounded so a chatty
// pod cannot blow up the request.
func Gather(ctx context.Context, sess *cluster.Session, kind kinds.Kind, namespace, name string) (*Evidence, error) {
	// Redacted: this leaves the machine for a third-party API. The owner
	// reads unredacted YAML in the interface; the model does not need it.
	obj, err := watch.GetRedacted(ctx, sess, kind, namespace, name)
	if err != nil {
		return nil, err
	}
	if len(obj) > 24000 {
		obj = obj[:24000] + "\n# (truncated)\n"
	}
	ev := &Evidence{Object: obj}
	if events, err := watch.EventsFor(ctx, sess, namespace, kind.Kind, name); err == nil {
		for i, e := range events {
			if i == 25 {
				break
			}
			ev.Events = append(ev.Events, fmt.Sprintf("%s %s x%d: %s", e.Type, e.Reason, e.Count, e.Message))
		}
	}
	if kind.Key == "pods" {
		ev.Logs = watch.TailLogs(ctx, sess, namespace, name, 120)
	}
	return ev, nil
}

// Ask sends the evidence to Claude and returns the answer.
func Ask(ctx context.Context, kindName, namespace, name string, ev *Evidence) (string, error) {
	key := keyFor("anthropic")
	if key == "" {
		return "", fmt.Errorf("no model key: add one in ClusterTrail, or set ANTHROPIC_API_KEY before starting the engine")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Object: %s %s/%s\n\n--- object ---\n%s\n", kindName, namespace, name, ev.Object)
	if len(ev.Events) > 0 {
		fmt.Fprintf(&b, "\n--- events, newest first ---\n%s\n", strings.Join(ev.Events, "\n"))
	}
	if len(ev.Logs) > 0 {
		fmt.Fprintf(&b, "\n--- recent log lines ---\n%s\n", strings.Join(ev.Logs, "\n"))
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	client := anthropic.NewClient(option.WithAPIKey(key))
	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     "claude-opus-5",
		MaxTokens: 2000,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(b.String()))},
	})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			out.WriteString(t.Text)
		}
	}
	if out.Len() == 0 {
		return "", fmt.Errorf("the model returned nothing (stop reason %s)", resp.StopReason)
	}
	return out.String(), nil
}

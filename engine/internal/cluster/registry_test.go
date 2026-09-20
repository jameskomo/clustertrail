package cluster

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The MCP server's promise is not "the tools do not write", which has to be
// re-established every time someone adds a tool. It is "the session cannot
// write", which holds whatever the tools do.
func TestReadOnlySessionRefusesAnythingButReadsAndDryRuns(t *testing.T) {
	var reached []string
	upstream := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		reached = append(reached, r.Method+" "+r.URL.RequestURI())
		return httptest.NewRecorder().Result(), nil
	})
	rt := readOnlyTransport(upstream)

	for _, ok := range []struct{ method, url string }{
		{"GET", "https://api/apis/apps/v1/namespaces/shop/deployments"},
		{"PUT", "https://api/apis/apps/v1/namespaces/shop/deployments/web?dryRun=All"},
	} {
		req, _ := http.NewRequest(ok.method, ok.url, nil)
		if _, err := rt.RoundTrip(req); err != nil {
			t.Errorf("%s %s should be allowed: %v", ok.method, ok.url, err)
		}
	}

	for _, bad := range []struct{ method, url string }{
		{"PUT", "https://api/apis/apps/v1/namespaces/shop/deployments/web"},
		{"PATCH", "https://api/apis/apps/v1/namespaces/shop/deployments/web"},
		{"DELETE", "https://api/api/v1/namespaces/shop/secrets/credentials"},
		{"POST", "https://api/api/v1/namespaces/shop/pods/web/exec"},
		{"PUT", "https://api/apis/apps/v1/namespaces/shop/deployments/web?dryRun=None"},
	} {
		req, _ := http.NewRequest(bad.method, bad.url, nil)
		_, err := rt.RoundTrip(req)
		if err == nil {
			t.Errorf("%s %s must be refused", bad.method, bad.url)
			continue
		}
		if !strings.Contains(err.Error(), "read-only") {
			t.Errorf("the refusal should say why, got %v", err)
		}
	}

	if len(reached) != 2 {
		t.Errorf("only the read and the dry run should have reached the API server, got %v", reached)
	}
}

package registry

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEditDistance(t *testing.T) {
	if got := editDistance("wago-org/asi", "wago-org/wasi"); got != 1 {
		t.Fatalf("edit distance = %d, want 1", got)
	}
	if got := editDistance("wasi", "wasi"); got != 0 {
		t.Fatalf("identical edit distance = %d, want 0", got)
	}
}

func TestClosestModule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/packages" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		_, _ = output.Write([]byte(`{"packages":[{"id":"wago-org/wasi"},{"id":"acme/logger"}]}`))
	}))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)

	if got := closestModule("github.com/wago-org/asi"); got != "wago-org/wasi" {
		t.Fatalf("closest module = %q, want wago-org/wasi", got)
	}
	if got := closestModule("github.com/unrelated/package"); got != "" {
		t.Fatalf("unrelated suggestion = %q, want none", got)
	}
}

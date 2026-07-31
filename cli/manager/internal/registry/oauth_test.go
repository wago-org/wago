package registry

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOAuthHelpers(t *testing.T) {
	state, err := RandomState()
	if err != nil || len(state) != 32 {
		t.Fatalf("RandomState = %q, %v", state, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	var reply struct {
		OK bool `json:"ok"`
	}
	if err := PostForm(server.URL, url.Values{"scope": {"read write"}}, &reply); err != nil || !reply.OK {
		t.Fatalf("PostForm = %+v, %v", reply, err)
	}
	if !strings.Contains(SuccessHTML, "logged in") {
		t.Fatal("login success HTML missing confirmation")
	}
}

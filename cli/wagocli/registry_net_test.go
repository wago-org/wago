//go:build !wago_lean

package wagocli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryHTTPHelpers(t *testing.T) {
	var gotAuth, gotContentType, gotBody string
	status := http.StatusOK
	body := `{"id":"42","login":"alice"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotContentType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		data, _ := ioReadAll(r)
		gotBody = string(data)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)

	code, data, err := apiRequest(http.MethodPost, "/x", "token", map[string]string{"name": "value"})
	if err != nil || code != http.StatusOK || string(data) != body {
		t.Fatalf("apiRequest = %d %q %v", code, data, err)
	}
	if gotAuth != "Bearer token" || gotContentType != "application/json" || gotBody != `{"name":"value"}` {
		t.Fatalf("request headers/body = %q %q %q", gotAuth, gotContentType, gotBody)
	}
	if _, _, err := apiRequest(http.MethodPost, "/x", "", map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("apiRequest accepted an unmarshalable body")
	}
	me, err := fetchMe("token")
	if err != nil || me.Login != "alice" {
		t.Fatalf("fetchMe = %+v, %v", me, err)
	}
	status, body = http.StatusUnauthorized, ""
	if _, err := fetchMe("token"); err != errUnauthorized {
		t.Fatalf("unauthorized fetchMe = %v", err)
	}
	status, body = http.StatusInternalServerError, `{"error":"broken"}`
	if _, err := fetchMe("token"); err == nil || err.Error() != "broken" {
		t.Fatalf("error fetchMe = %v", err)
	}
	status, body = http.StatusOK, "not json"
	if _, err := fetchMe("token"); err == nil {
		t.Fatal("invalid JSON fetchMe accepted")
	}
	if got := apiError(http.StatusBadRequest, []byte("not json")); got != "server returned status 400" {
		t.Fatalf("apiError fallback = %q", got)
	}
}

// ioReadAll makes the handler's read error irrelevant to the request assertions.
func ioReadAll(r *http.Request) ([]byte, error) { return io.ReadAll(r.Body) }

func TestRegistryFilesystemHelpers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 1025), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "ignored"), make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".wago"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".wago", "ignored"), make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := walkedSize(dir); got != 1025 {
		t.Fatalf("walkedSize = %d", got)
	}
	if got := gitTrackedSize(dir); got != -1 {
		t.Fatalf("gitTrackedSize non-repo = %d", got)
	}
	if got := dirUnpackedKB(dir); got != 2 {
		t.Fatalf("dirUnpackedKB = %d", got)
	}
	if got := dirUnpackedKB(filepath.Join(dir, "missing")); got != 0 {
		t.Fatalf("missing dirUnpackedKB = %d", got)
	}
	if gitOutput("definitely-not-a-git-command") != "" {
		t.Fatal("failed gitOutput was non-empty")
	}
	name, version := splitAtVersion("example/pkg@v1.2.3")
	if name != "example/pkg" || version != "v1.2.3" {
		t.Fatalf("splitAtVersion = %q %q", name, version)
	}
	name, version = splitAtVersion("a@b@v1")
	if name != "a@b" || version != "v1" {
		t.Fatalf("splitAtVersion = %q %q", name, version)
	}
	if got := strings.Join(splitCommaList(" a, ,b ,, c "), ","); got != "a,b,c" {
		t.Fatalf("splitCommaList = %q", got)
	}
}

func TestInlineManifestAndRegistryModuleResolution(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.json")
	grandchild := filepath.Join(dir, "grandchild.json")
	if err := os.WriteFile(grandchild, []byte(`{"name":"grandchild"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte(`{"name":"child","subpackages":["./grandchild.json"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	inline, err := inlineManifest([]byte(`{"name":"root","subpackages":["./child.json", {"name":"inline"}]}`), dir)
	if err != nil || !strings.Contains(string(inline), `"grandchild"`) || strings.Contains(string(inline), "./child.json") {
		t.Fatalf("inlineManifest = %s, %v", inline, err)
	}
	if _, err := inlineManifest([]byte("not json"), dir); err == nil {
		t.Fatal("invalid manifest accepted")
	}
	if _, err := inlineManifest([]byte(`{"subpackages":["missing.json"]}`), dir); err == nil {
		t.Fatal("missing subpackage accepted")
	}
	if err := os.WriteFile(child, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inlineManifest([]byte(`{"subpackages":["child.json"]}`), dir); err == nil {
		t.Fatal("invalid child accepted")
	}

	status, body := http.StatusOK, `{"name":"github.com/acme/plugin"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)
	if got, err := resolveRegistryModule("a b"); err != nil || got != "github.com/acme/plugin" {
		t.Fatalf("resolveRegistryModule = %q, %v", got, err)
	}
	status, body = http.StatusNotFound, ""
	if _, err := resolveRegistryModule("missing"); err == nil || !strings.Contains(err.Error(), "no plugin") {
		t.Fatalf("not found = %v", err)
	}
	status, body = http.StatusBadGateway, `{"error":"down"}`
	if _, err := resolveRegistryModule("bad"); err == nil || err.Error() != "down" {
		t.Fatalf("server error = %v", err)
	}
	status, body = http.StatusOK, "not json"
	if _, err := resolveRegistryModule("bad-json"); err == nil {
		t.Fatal("invalid resolution JSON accepted")
	}
	status, body = http.StatusOK, `{}`
	if _, err := resolveRegistryModule("empty"); err == nil || !strings.Contains(err.Error(), "no module path") {
		t.Fatalf("empty module path = %v", err)
	}
}

func TestRegistrySessionCommandsAndOAuthHelpers(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("HOME", "")
	t.Setenv("WAGO_TOKEN", "")
	status, body := http.StatusOK, `{"login":"alice"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/form" {
			if err := r.ParseForm(); err != nil || r.Form.Get("scope") != "read write" {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)
	registryWhoami(nil) // no token
	registryLogout(nil) // no credentials
	if err := saveCredentials(server.URL, "stored", "alice"); err != nil {
		t.Fatal(err)
	}
	registryWhoami(nil)
	status, body = http.StatusUnauthorized, ""
	registryWhoami(nil)
	status, body = http.StatusOK, `{"login":"alice"}`
	registryLogout(nil)
	if got := resolveToken(); got != "" {
		t.Fatalf("logout left token %q", got)
	}
	state, err := randomState()
	if err != nil || len(state) != 32 {
		t.Fatalf("randomState = %q, %v", state, err)
	}
	var reply struct {
		OK bool `json:"ok"`
	}
	if err := githubForm(server.URL+"/form", url.Values{"scope": {"read write"}}, &reply); err != nil || !reply.OK {
		t.Fatalf("githubForm = %+v, %v", reply, err)
	}
	if !strings.Contains(loginSuccessHTML, "logged in") {
		t.Fatal("login success HTML missing confirmation")
	}
}

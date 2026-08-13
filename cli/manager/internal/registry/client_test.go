package registry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/httpclient"
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
	if _, err := fetchMe("token"); err == nil || err.Error() != "registry returned status 500" {
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

func TestAuthenticatedRegistryErrorsDiscardReflectedCredentials(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusInternalServerError,
		http.StatusTemporaryRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(status)
				_, _ = writer.Write([]byte(`{"error":"` + testRegistryToken + `"}`))
			}))
			defer server.Close()

			gotStatus, data, err := apiRequestAtBaseContext(context.Background(), server.URL, http.MethodGet,
				"/api/me", testRegistryToken, nil)
			if err != nil || gotStatus != status {
				t.Fatalf("authenticated failure = %d, %v", gotStatus, err)
			}
			if len(data) != 0 {
				t.Fatalf("authenticated failure retained reflected body %q", data)
			}
		})
	}
}

func TestRegistryIdentityAndErrorsAreTerminalSafe(t *testing.T) {
	for _, body := range []string{
		`{"login":"alice\u001b[2J"}`,
		`{"login":"alice\nforged"}`,
		`{"login":"` + strings.Repeat("x", maximumRegistryLoginLength+1) + `"}`,
		`{"login":""}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(body))
		}))
		_, err := fetchMeAtBaseContext(context.Background(), server.URL, testRegistryToken)
		server.Close()
		if err == nil {
			t.Fatalf("accepted unsafe identity %s", body)
		}
	}

	for _, data := range []string{
		`{"error":"broken\u001b[2J"}`,
		`{"error":"broken\nforged"}`,
		`{"error":"` + strings.Repeat("x", 1025) + `"}`,
	} {
		if got := apiError(http.StatusBadRequest, []byte(data)); got != "server returned status 400" {
			t.Fatalf("unsafe registry error rendered as %q", got)
		}
	}
	if got := apiError(http.StatusBadRequest, []byte(`{"error":"ordinary failure"}`)); got != "ordinary failure" {
		t.Fatalf("safe registry error = %q", got)
	}
}

func TestRegistryIdentityRejectsAmbiguousAndReflectedJSON(t *testing.T) {
	remote := strings.Repeat("9", 32<<10)
	for _, body := range []string{
		`{"login":"alice","Login":"mallory"}`,
		`{"login":` + remote + `}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(body))
		}))
		_, err := fetchMeAtBaseContext(context.Background(), server.URL, testRegistryToken)
		server.Close()
		if err == nil {
			t.Fatalf("ambiguous registry identity succeeded: %.80s", body)
		}
		if strings.Contains(err.Error(), remote[:1024]) || len(err.Error()) > 256 {
			t.Fatalf("registry identity error was not bounded: %d bytes", len(err.Error()))
		}
	}
}

func TestRegistryDisplayBaseIsTerminalSafeAndBounded(t *testing.T) {
	const valid = "https://registry.example/prefix"
	if got := registryDisplayBase(valid); got != valid {
		t.Fatalf("valid registry display = %q", got)
	}
	for _, base := range []string{
		"https://registry.example/failure\nforged",
		"https://registry.example/failure\x1b[2J",
		"https://secret@registry.example",
		"https://registry.example/" + strings.Repeat("x", maximumRegistryDisplayURLLength),
	} {
		if got := registryDisplayBase(base); got != "configured registry" {
			t.Fatalf("unsafe registry display of length %d = %q", len(base), got)
		}
	}
}

func TestRegistryBaseURLSecurityPolicy(t *testing.T) {
	for _, base := range []string{
		"https://registry.example",
		"https://registry.example:8443/prefix",
		"http://localhost:8080",
		"http://api.localhost:8080",
		"http://127.0.0.1:8080",
		"http://127.255.255.254:8080",
		"http://[::1]:8080",
	} {
		if err := validateRegistryBaseURL(base); err != nil {
			t.Errorf("validateRegistryBaseURL(%q) = %v", base, err)
		}
	}
	for _, base := range []string{
		"http://registry.example",
		"http://localhost.example",
		"ftp://registry.example",
		"registry.example",
		"https://user:password@registry.example",
		"https://registry.example?destination=evil",
		"https://registry.example#fragment",
	} {
		if err := validateRegistryBaseURL(base); err == nil {
			t.Errorf("validateRegistryBaseURL(%q) succeeded", base)
		}
	}
}

func TestRegistryRequestRejectsRemotePlaintextBeforeDial(t *testing.T) {
	_, _, err := apiRequestAtBaseContext(context.Background(), "http://192.0.2.1", http.MethodPost,
		"/api/auth/github/exchange", "", map[string]string{"access_token": testGitHubToken})
	if err == nil || !strings.Contains(err.Error(), "HTTPS is required") {
		t.Fatalf("remote plaintext registry = %v", err)
	}
}

func TestRegistryRequestRejectsUnsafeBearerBeforeDial(t *testing.T) {
	for _, token := range []string{
		"token\nforged",
		"token\x1b[2J",
		strings.Repeat("x", maximumRegistryTokenLength+1),
	} {
		_, _, err := apiRequestAtBaseContext(context.Background(), "https://192.0.2.1", http.MethodGet, "/api/me", token, nil)
		if err == nil {
			t.Fatalf("unsafe bearer of length %d reached request path", len(token))
		}
	}
}

func TestRegistryAuthenticationResponsesUseSmallerLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", strconv.FormatInt(registryAuthResponseLimit+1, 10))
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := fetchMeAtBaseContext(context.Background(), server.URL, testRegistryToken); !errors.Is(err, httpclient.ErrBodyTooLarge) {
		t.Fatalf("oversized auth response = %v", err)
	}
}

func TestFetchMeUsesPinnedRegistryOrigin(t *testing.T) {
	var receivedBearer string
	var unexpectedRequests atomic.Int32
	pinned := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedBearer = request.Header.Get("Authorization")
		_, _ = writer.Write([]byte(`{"login":"alice"}`))
	}))
	defer pinned.Close()

	unexpected := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		unexpectedRequests.Add(1)
	}))
	defer unexpected.Close()
	t.Setenv("WAGO_REGISTRY", unexpected.URL)

	me, err := fetchMeAtBaseContext(context.Background(), pinned.URL, testRegistryToken)
	if err != nil || me.Login != "alice" {
		t.Fatalf("fetchMeAtBaseContext = %+v, %v", me, err)
	}
	if receivedBearer != "Bearer "+testRegistryToken {
		t.Fatalf("pinned registry received Authorization %q", receivedBearer)
	}
	if got := unexpectedRequests.Load(); got != 0 {
		t.Fatalf("mutable registry received %d bearer validation requests", got)
	}
}

func TestRegistryExchangeRedirectDoesNotForwardCredential(t *testing.T) {
	for _, status := range []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var sinkRequests atomic.Int32
			sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				sinkRequests.Add(1)
			}))
			defer sink.Close()
			registry := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Location", sink.URL)
				writer.WriteHeader(status)
			}))
			defer registry.Close()

			gotStatus, _, err := apiRequestAtBaseContext(context.Background(), registry.URL, http.MethodPost,
				"/api/auth/github/exchange", testRegistryToken, map[string]string{"access_token": testGitHubToken})
			if err != nil || gotStatus != status {
				t.Fatalf("redirect response = %d, %v", gotStatus, err)
			}
			if got := sinkRequests.Load(); got != 0 {
				t.Fatalf("redirect sink received %d credential-bearing requests", got)
			}
		})
	}
}

func TestRegistryRequestIsBoundedAndCancelable(t *testing.T) {
	oldMaximum := registryResponseMaximum
	registryResponseMaximum = 32
	t.Cleanup(func() { registryResponseMaximum = oldMaximum })

	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(strings.Repeat("x", 33)))
	}))
	defer oversized.Close()
	t.Setenv("WAGO_REGISTRY", oversized.URL)
	if _, _, err := apiRequestContext(context.Background(), http.MethodGet, "/oversized", "", nil); !errors.Is(err, httpclient.ErrBodyTooLarge) {
		t.Fatalf("oversized registry response = %v", err)
	}

	stalled := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer stalled.Close()
	t.Setenv("WAGO_REGISTRY", stalled.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := apiRequestContext(ctx, http.MethodGet, "/stalled", "", nil)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled registry response = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled registry request did not return")
	}
}

func TestRecordRegistryInstall(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		data, _ := ioReadAll(r)
		gotBody = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("WAGO_REGISTRY", server.URL)

	recordRegistryInstall("github.com/acme/plugin", "v1.2.3")

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/packages/github.com%2Facme%2Fplugin/installs" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody != `{"version":"v1.2.3"}` {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestRecordRegistryInstallRejectsRemotePlaintextBeforeDial(t *testing.T) {
	oldClient := registryHTTP
	var requests atomic.Int32
	registryHTTP = httpclient.New(httpclient.Config{HTTPClient: &http.Client{Transport: registryRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected request")
	})}})
	t.Cleanup(func() { registryHTTP = oldClient })
	t.Setenv("WAGO_REGISTRY", "http://192.0.2.1")

	recordRegistryInstall("github.com/acme/plugin", "v1.2.3")
	if got := requests.Load(); got != 0 {
		t.Fatalf("remote plaintext install metric made %d requests", got)
	}
}

type registryRoundTripFunc func(*http.Request) (*http.Response, error)

func (function registryRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

// ioReadAll makes the handler's read error irrelevant to the request assertions.
func ioReadAll(r *http.Request) ([]byte, error) { return io.ReadAll(r.Body) }

func TestRegistryModuleResolution(t *testing.T) {
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
	registryWhoami() // no token
	registryLogout() // no credentials
	if err := saveCredentials(server.URL, "stored", "alice"); err != nil {
		t.Fatal(err)
	}
	registryWhoami()
	status, body = http.StatusUnauthorized, ""
	registryWhoami()
	status, body = http.StatusOK, `{"login":"alice"}`
	registryLogout()
	if got := resolveToken(); got != "" {
		t.Fatalf("logout left token %q", got)
	}
}

func TestLoginMethodPickerDefaultsToLinkAndAcceptsRightArrow(t *testing.T) {
	p := loginMethodPicker()
	if got := p.Selected(); got != "link" {
		t.Fatalf("default login method = %q, want link", got)
	}
	frame := p.Frame()
	for _, want := range []string{
		"Choose login method",
		"Link", "Open one-time authorization in a browser",
		"Code", "Use a one-time code on another device",
		"◉", "○", "enter/→ select", "esc cancel",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("login picker missing %q:\n%s", want, frame)
		}
	}
	if done, cancelled := p.MoveDown(); done || cancelled {
		t.Fatalf("down = done %v, cancelled %v", done, cancelled)
	}
	if got := p.Selected(); got != "code" {
		t.Fatalf("selected login method = %q, want code", got)
	}
	if done, cancelled := p.SelectRight(); !done || cancelled {
		t.Fatalf("right = done %v, cancelled %v", done, cancelled)
	}
}

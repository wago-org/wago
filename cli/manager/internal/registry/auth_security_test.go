package registry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testDeviceCode    = "device-secret"
	testGitHubToken   = "github-access-secret"
	testRegistryToken = "registry-bearer-secret"
)

type deviceFlowCapture struct {
	requestURLs          []string
	deviceForm           url.Values
	pollForm             url.Values
	exchangedGitHubToken string
	verificationURL      string
}

func newDeviceFlowServer(t *testing.T, accessTokenBody string, exchangeStatus int, exchangeBody string) (*httptest.Server, *deviceFlowCapture) {
	t.Helper()
	capture := &deviceFlowCapture{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		capture.requestURLs = append(capture.requestURLs, request.URL.String())
		switch request.URL.Path {
		case "/api/auth/github/client":
			_ = json.NewEncoder(w).Encode(map[string]string{"client_id": "client-id", "scope": "read:user"})
		case "/login/device/code":
			_ = request.ParseForm()
			capture.deviceForm = cloneURLValues(request.Form)
			capture.verificationURL = server.URL + "/login/device?user_code=short-lived-code"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":               testDeviceCode,
				"user_code":                 "short-lived-code",
				"verification_uri":          server.URL + "/login/device",
				"verification_uri_complete": capture.verificationURL,
				"expires_in":                600,
				"interval":                  1,
			})
		case "/login/oauth/access_token":
			_ = request.ParseForm()
			capture.pollForm = cloneURLValues(request.Form)
			_, _ = w.Write([]byte(accessTokenBody))
		case "/api/auth/github/exchange":
			var body map[string]string
			_ = json.NewDecoder(request.Body).Decode(&body)
			capture.exchangedGitHubToken = body["access_token"]
			w.WriteHeader(exchangeStatus)
			_, _ = w.Write([]byte(exchangeBody))
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)
	return server, capture
}

func cloneURLValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
}

func deviceFlowTestHooks(t *testing.T, serverURL string, openedURL *string) deviceFlowHooks {
	t.Helper()
	return deviceFlowHooks{
		deviceCodeEndpoint:  serverURL + "/login/device/code",
		accessTokenEndpoint: serverURL + "/login/oauth/access_token",
		openBrowser: func(target string) error {
			*openedURL = target
			return nil
		},
		wait: func(ctx context.Context, _ time.Duration) error {
			return ctx.Err()
		},
	}
}

func TestDeviceAuthorizationKeepsCredentialsOutOfURLs(t *testing.T) {
	for _, test := range []struct {
		name        string
		openBrowser bool
	}{
		{name: "browser", openBrowser: true},
		{name: "headless", openBrowser: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, capture := newDeviceFlowServer(t, `{"access_token":"`+testGitHubToken+`"}`,
				http.StatusOK, `{"token":"`+testRegistryToken+`"}`)
			var openedURL string
			hooks := deviceFlowTestHooks(t, server.URL, &openedURL)

			token, err := githubDeviceTokenUsingContext(context.Background(), server.URL, test.openBrowser, hooks)
			if err != nil || token != testRegistryToken {
				t.Fatalf("device token = %q, %v", token, err)
			}
			if capture.deviceForm.Get("client_id") != "client-id" || capture.pollForm.Get("device_code") != testDeviceCode {
				t.Fatalf("device forms = %#v / %#v", capture.deviceForm, capture.pollForm)
			}
			if capture.exchangedGitHubToken != testGitHubToken {
				t.Fatalf("exchanged token = %q", capture.exchangedGitHubToken)
			}
			if test.openBrowser {
				if openedURL != capture.verificationURL {
					t.Fatalf("opened URL = %q, want %q", openedURL, capture.verificationURL)
				}
			} else if openedURL != "" {
				t.Fatalf("headless login opened %q", openedURL)
			}
			for _, visibleURL := range append(capture.requestURLs, openedURL) {
				for _, secret := range []string{testDeviceCode, testGitHubToken, testRegistryToken} {
					if strings.Contains(visibleURL, secret) {
						t.Fatalf("URL %q leaked %q", visibleURL, secret)
					}
				}
				if strings.Contains(visibleURL, "token=") || strings.Contains(visibleURL, "/callback") {
					t.Fatalf("URL retained insecure callback shape: %q", visibleURL)
				}
			}
		})
	}
}

func TestDeviceExchangeErrorRedactsCredentials(t *testing.T) {
	server, _ := newDeviceFlowServer(t, `{"access_token":"`+testGitHubToken+`"}`,
		http.StatusInternalServerError,
		`{"error":"`+testDeviceCode+` `+testGitHubToken+` `+testRegistryToken+`"}`)
	var openedURL string
	hooks := deviceFlowTestHooks(t, server.URL, &openedURL)

	_, err := githubDeviceTokenUsingContext(context.Background(), server.URL, false, hooks)
	if err == nil {
		t.Fatal("failed exchange succeeded")
	}
	for _, secret := range []string{testDeviceCode, testGitHubToken, testRegistryToken} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}

func TestDevicePollingErrorRedactsCredentials(t *testing.T) {
	server, capture := newDeviceFlowServer(t,
		`{"error":"`+testDeviceCode+` `+testGitHubToken+` `+testRegistryToken+`"}`,
		http.StatusOK, `{"token":"`+testRegistryToken+`"}`)
	var openedURL string
	hooks := deviceFlowTestHooks(t, server.URL, &openedURL)

	_, err := githubDeviceTokenUsingContext(context.Background(), server.URL, false, hooks)
	if err == nil {
		t.Fatal("failed token poll succeeded")
	}
	for _, secret := range []string{testDeviceCode, testGitHubToken, testRegistryToken} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
	if capture.exchangedGitHubToken != "" {
		t.Fatalf("failed poll exchanged %q", capture.exchangedGitHubToken)
	}
}

func TestDeviceAuthorizationCancellationStopsBeforeTokenPolling(t *testing.T) {
	server, capture := newDeviceFlowServer(t, `{"access_token":"`+testGitHubToken+`"}`,
		http.StatusOK, `{"token":"`+testRegistryToken+`"}`)
	var openedURL string
	hooks := deviceFlowTestHooks(t, server.URL, &openedURL)
	hooks.wait = func(context.Context, time.Duration) error { return context.Canceled }

	_, err := githubDeviceTokenUsingContext(context.Background(), server.URL, false, hooks)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled device flow = %v", err)
	}
	if capture.pollForm != nil || capture.exchangedGitHubToken != "" {
		t.Fatalf("canceled flow polled or exchanged a credential: %#v / %q", capture.pollForm, capture.exchangedGitHubToken)
	}
}

func TestDeviceAuthorizationPinsRegistryOrigin(t *testing.T) {
	server, capture := newDeviceFlowServer(t, `{"access_token":"`+testGitHubToken+`"}`,
		http.StatusOK, `{"token":"`+testRegistryToken+`"}`)
	var openedURL string
	hooks := deviceFlowTestHooks(t, server.URL, &openedURL)
	t.Setenv("WAGO_REGISTRY", "http://127.0.0.1:1")

	token, err := githubDeviceTokenUsingContext(context.Background(), server.URL, false, hooks)
	if err != nil || token != testRegistryToken {
		t.Fatalf("pinned registry token = %q, %v", token, err)
	}
	if capture.exchangedGitHubToken != testGitHubToken {
		t.Fatalf("pinned registry did not receive exchange token: %q", capture.exchangedGitHubToken)
	}
}

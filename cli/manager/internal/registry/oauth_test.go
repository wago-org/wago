package registry

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/httpclient"
)

func TestDeviceFlowTimingIsBounded(t *testing.T) {
	for _, test := range []struct {
		name                string
		expiresIn, interval int
		wantLifetime        time.Duration
		wantInterval        time.Duration
	}{
		{name: "defaults", wantLifetime: defaultDeviceFlowLifetime, wantInterval: defaultDevicePollInterval},
		{name: "server values", expiresIn: 600, interval: 10, wantLifetime: 10 * time.Minute, wantInterval: 10 * time.Second},
		{name: "huge values", expiresIn: int(^uint(0) >> 1), interval: int(^uint(0) >> 1), wantLifetime: maximumDeviceFlowLifetime, wantInterval: maximumDevicePollInterval},
	} {
		t.Run(test.name, func(t *testing.T) {
			lifetime, interval := deviceFlowTiming(test.expiresIn, test.interval)
			if lifetime != test.wantLifetime || interval != test.wantInterval {
				t.Fatalf("device timing = %v/%v, want %v/%v", lifetime, interval, test.wantLifetime, test.wantInterval)
			}
		})
	}
}

func TestOAuthHelpers(t *testing.T) {
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
	oldMaximum := oauthResponseMaximum
	oauthResponseMaximum = 16
	t.Cleanup(func() { oauthResponseMaximum = oldMaximum })
	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte(strings.Repeat("x", 17)))
	}))
	defer oversized.Close()
	if err := PostForm(oversized.URL, url.Values{}, &reply); !errors.Is(err, httpclient.ErrBodyTooLarge) {
		t.Fatalf("oversized OAuth response = %v", err)
	}
}

func TestOAuthJSONRejectsDuplicateObjectMembers(t *testing.T) {
	for _, body := range []string{
		`{"access_token":"trusted","error":"access_denied","error":""}`,
		`{"access_token":"trusted","error":"access_denied","Error":""}`,
		`{"scope":"read:user","ſcope":"repo"}`,
		`{"outer":{"error":"access_denied","error":""}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		var reply map[string]any
		err := PostForm(server.URL, url.Values{}, &reply)
		server.Close()
		if err == nil || !strings.Contains(err.Error(), "duplicate object field") {
			t.Fatalf("PostForm(%s) = %v", body, err)
		}
	}
}

func TestOAuthJSONErrorsDoNotReflectRemoteValues(t *testing.T) {
	remote := strings.Repeat("9", 32<<10)
	for _, body := range []string{
		`{"expires_in":` + remote + `}`,
		`{"expires_in":1e` + remote + `}`,
	} {
		var reply struct {
			ExpiresIn int `json:"expires_in"`
		}
		err := unmarshalUniqueJSON([]byte(body), &reply)
		if err == nil {
			t.Fatal("malformed remote number succeeded")
		}
		if strings.Contains(err.Error(), remote[:1024]) || len(err.Error()) > 256 {
			t.Fatalf("remote JSON error was not bounded: %d bytes", len(err.Error()))
		}
	}
}

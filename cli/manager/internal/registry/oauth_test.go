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

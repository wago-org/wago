package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/tui"
	"github.com/wago-org/wago/cli/internal/ui"
)

func registryLogin(options LoginRequest) {
	registryLoginContext(context.Background(), options)
}

func registryLoginContext(ctx context.Context, options LoginRequest) {
	withToken := options.WithToken
	code := options.Code
	link := options.Link
	token := options.Token
	base := registryBase()
	switch {
	case token != "":
		// use the provided token directly
	case withToken:
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal("login: reading token from stdin: %v", err)
		}
		token = strings.TrimSpace(string(b))
		if token == "" {
			fatal("login: no token on stdin")
		}
	case code:
		token = githubDeviceLoginContext(ctx, base)
	case link:
		token = browserLoginContext(ctx, base)
	default:
		var ok bool
		token, ok = chooseLoginMethodContext(ctx, base)
		if !ok {
			return
		}
	}
	me, err := fetchMeContext(ctx, token)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			fatal("login: the registry rejected that token")
		}
		fatal("login: %v", err)
	}
	if err := saveCredentialsContext(ctx, base, token, me.Login); err != nil {
		fatal("login: saving credentials: %v", err)
	}
	fmt.Printf("%s Logged in as %s\n", cyan("✓"), bold(me.Login))
}

func loginMethodPicker() *tui.Picker {
	return tui.NewPicker("Choose login method", []tui.Item{
		{
			Label:       "Link",
			Description: "Open a browser link on this machine",
			Value:       "link",
		},
		{
			Label:       "Code",
			Description: "Use a one-time code on another device",
			Value:       "code",
		},
	})
}

// chooseLoginMethodContext asks how to log in using the shared radio selector.
// Link is selected by default and remains the fallback for non-interactive callers.
func chooseLoginMethodContext(ctx context.Context, base string) (string, bool) {
	p := loginMethodPicker()
	submitted, cancelled := tui.Run(p)
	if cancelled {
		return "", false
	}
	method := p.Selected()
	if !submitted {
		method = "link"
	}
	switch method {
	case "code":
		return githubDeviceLoginContext(ctx, base), true
	default:
		return browserLoginContext(ctx, base), true
	}
}

// browserLoginContext runs the loopback OAuth flow: it listens on a free
// localhost port, opens the browser to the registry's CLI-login endpoint, and
// waits for the /callback redirect carrying the plaintext token. It fatals on
// error or timeout.
func browserLoginContext(ctx context.Context, base string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal("login: cannot open a loopback listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	state, err := RandomState()
	if err != nil {
		fatal("login: %v", err)
	}

	type result struct {
		token string
		err   error
	}
	resCh := make(chan result, 1)
	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch — login aborted", http.StatusBadRequest)
			resCh <- result{err: errors.New("state mismatch — login aborted (possible CSRF)")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, SuccessHTML)
		resCh <- result{token: q.Get("token")}
	})
	go srv.Serve(ln)
	defer srv.Close()

	loginURL := fmt.Sprintf("%s/auth/cli/login?port=%d&state=%s", base, port, url.QueryEscape(state))
	if err := OpenBrowser(loginURL); err != nil {
		fmt.Printf("Open this URL in your browser to log in:\n\n  %s\n\n", cyan(loginURL))
	} else {
		fmt.Printf("%s Opening your browser to log in…\n", dim("→"))
		fmt.Printf("  %s\n  %s\n", dim("if it doesn't open, visit:"), cyan(loginURL))
	}

	select {
	case res := <-resCh:
		if res.err != nil {
			fatal("login: %v", res.err)
		}
		if res.token == "" {
			fatal("login: no token received from the registry")
		}
		return res.token
	case <-ctx.Done():
		fatal("login: %v", ctx.Err())
		return ""
	case <-time.After(2 * time.Minute):
		fatal("login: timed out waiting for the browser callback")
		return ""
	}
}

const (
	defaultDeviceFlowLifetime = 15 * time.Minute
	maximumDeviceFlowLifetime = 20 * time.Minute
	defaultDevicePollInterval = 5 * time.Second
	maximumDevicePollInterval = 60 * time.Second
)

func deviceFlowTiming(expiresIn, interval int) (lifetime, pollInterval time.Duration) {
	lifetime = defaultDeviceFlowLifetime
	if expiresIn > 0 {
		if expiresIn > int(maximumDeviceFlowLifetime/time.Second) {
			lifetime = maximumDeviceFlowLifetime
		} else {
			lifetime = time.Duration(expiresIn) * time.Second
		}
	}
	pollInterval = defaultDevicePollInterval
	if interval > 0 {
		if interval > int(maximumDevicePollInterval/time.Second) {
			pollInterval = maximumDevicePollInterval
		} else {
			pollInterval = time.Duration(interval) * time.Second
		}
	}
	return lifetime, pollInterval
}

// githubDeviceLoginContext runs GitHub's OAuth device flow (RFC 8628) and
// exchanges the resulting GitHub token for a wago API token. It's the login path
// for headless/remote machines: the user enters a code at github.com/login/device
// instead of relying on a localhost redirect.
//
// The registry advertises its GitHub OAuth client_id (GET /api/auth/github/client)
// so a self-hosted registry with its own OAuth app works without recompiling the
// CLI. The CLI talks to GitHub directly for the device + access token, then hands
// the GitHub token to the registry (POST /api/auth/github/exchange), which
// verifies the token belongs to its app and returns a wago token.
func githubDeviceLoginContext(ctx context.Context, base string) string {
	// 1. Ask the registry which GitHub OAuth app to authenticate against.
	status, data, err := apiRequestContext(ctx, http.MethodGet, "/api/auth/github/client", "", nil)
	if err != nil {
		fatal("login: fetching GitHub client config: %v", err)
	}
	if status != http.StatusOK {
		fatal("login: fetching GitHub client config: %s", apiError(status, data))
	}
	var cfg struct {
		ClientID string `json:"client_id"`
		Scope    string `json:"scope"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fatal("login: parsing GitHub client config: %v", err)
	}
	if cfg.ClientID == "" {
		fatal("login: registry did not advertise a GitHub client id")
	}

	// 2. Ask GitHub for a device + user code.
	var dc struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
	}
	if err := PostFormContext(ctx, "https://github.com/login/device/code",
		url.Values{"client_id": {cfg.ClientID}, "scope": {cfg.Scope}}, &dc); err != nil {
		fatal("login: requesting device code from GitHub: %v", err)
	}
	if dc.Error == "device_flow_disabled" {
		fatal("login: GitHub device flow is disabled for this OAuth app — enable \"Device Flow\" in its settings")
	}
	if dc.DeviceCode == "" || dc.UserCode == "" {
		if dc.Error != "" {
			fatal("login: GitHub: %s", dc.Error)
		}
		fatal("login: GitHub returned an incomplete device authorization response")
	}

	lifetime, pollInterval := deviceFlowTiming(dc.ExpiresIn, dc.Interval)
	verifyURI := dc.VerificationURI
	if verifyURI == "" {
		verifyURI = "https://github.com/login/device"
	}

	// Keep the terminal still so the code can be copied before the user opens a
	// browser. Unlike Link login, the device flow never launches one itself.
	fmt.Printf("\n  First, copy your one-time code:\n\n      %s\n\n", bold(dc.UserCode))
	fmt.Printf("  Then open %s and enter it.\n", cyan(verifyURI))
	fmt.Printf("\n%s Waiting for you to authorize on GitHub…\n", dim("→"))

	// 3. Poll GitHub for the access token until the user authorizes or it expires.
	var ghToken string
	deadline := time.Now().Add(lifetime)
	for time.Now().Before(deadline) {
		wait := min(pollInterval, time.Until(deadline))
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			fatal("login: %v", ctx.Err())
		case <-timer.C:
		}
		if !time.Now().Before(deadline) {
			break
		}
		var tr struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		if err := PostFormContext(ctx, "https://github.com/login/oauth/access_token", url.Values{
			"client_id":   {cfg.ClientID},
			"device_code": {dc.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &tr); err != nil {
			fatal("login: polling GitHub for the token: %v", err)
		}
		if tr.AccessToken != "" {
			ghToken = tr.AccessToken
			break
		}
		switch tr.Error {
		case "authorization_pending":
			// not authorized yet — keep polling
		case "slow_down":
			// RFC 8628 §3.5 asks for a five-second backoff. Keep the
			// server-controlled interval within the command's bounded policy.
			pollInterval = min(pollInterval+5*time.Second, maximumDevicePollInterval)
		case "access_denied":
			fatal("login: authorization was denied on GitHub")
		case "expired_token":
			fatal("login: the code expired before you authorized it — run `wago auth login --code` again")
		default:
			fatal("login: GitHub: %s", tr.Error)
		}
	}
	if ghToken == "" {
		fatal("login: timed out waiting for GitHub authorization")
	}

	// 4. Exchange the GitHub token for a wago API token.
	status, data, err = apiRequestContext(ctx, http.MethodPost, "/api/auth/github/exchange", "",
		map[string]string{"access_token": ghToken})
	if err != nil {
		fatal("login: exchanging GitHub token: %v", err)
	}
	if status != http.StatusOK {
		fatal("login: exchanging GitHub token: %s", apiError(status, data))
	}
	var xr struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &xr); err != nil {
		fatal("login: parsing exchange response: %v", err)
	}
	if xr.Token == "" {
		fatal("login: registry returned no token after the GitHub exchange")
	}
	return xr.Token
}

// registryLogout deletes stored credentials for the current registry.
func registryLogout() {
	registryLogoutContext(context.Background())
}

func registryLogoutContext(ctx context.Context) {
	base := registryBase()
	removed, err := deleteCredentialsResultContext(ctx, base)
	if err != nil {
		fatal("logout: %v", err)
	}
	if !removed {
		fmt.Printf("%s Not logged in to %s\n", dim("·"), base)
		return
	}
	fmt.Printf("%s Logged out of %s\n", cyan("✓"), base)
}

// registryWhoami prints the login of the current token, or a friendly hint when
// there is no valid session.
func registryWhoami() {
	registryWhoamiContext(context.Background())
}

func registryWhoamiContext(ctx context.Context) {
	token := resolveToken()
	if token == "" {
		if automation.JSON() {
			ui.PrintJSON(map[string]any{"authenticated": false, "registry": registryBase()})
			return
		}
		fmt.Println("not logged in (run: wago auth login)")
		return
	}
	me, err := fetchMeContext(ctx, token)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			if automation.JSON() {
				ui.PrintJSON(map[string]any{"authenticated": false, "registry": registryBase()})
				return
			}
			fmt.Println("not logged in (run: wago auth login)")
			return
		}
		fatal("whoami: %v", err)
	}
	if automation.JSON() {
		ui.PrintJSON(map[string]any{"authenticated": true, "registry": registryBase(), "login": me.Login})
		return
	}
	fmt.Println(me.Login)
}

// resolveRegistryModule looks up a package by its short name on the registry and
// returns its Go module path, so `wago add <name>` accepts a short name and
// not only a full module path.

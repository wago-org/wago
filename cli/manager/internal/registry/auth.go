package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		var err error
		token, err = readRegistryTokenContext(ctx, os.Stdin)
		if err != nil {
			fatal("login: reading token from stdin: %v", err)
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
	if err := validateRegistryToken(token); err != nil {
		fatal("login: %v", err)
	}
	me, err := fetchMeAtBaseContext(ctx, base, token)
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

func readRegistryToken(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maximumRegistryInputLength+1))
	if err != nil {
		return "", err
	}
	if len(data) > maximumRegistryInputLength {
		return "", fmt.Errorf("registry token exceeds %d-byte limit", maximumRegistryTokenLength)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("no token on stdin")
	}
	if err := validateRegistryToken(token); err != nil {
		return "", err
	}
	return token, nil
}

func readRegistryTokenContext(ctx context.Context, reader io.ReadCloser) (string, error) {
	if ctx == nil {
		return "", errors.New("nil registry token context")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type result struct {
		token string
		err   error
	}
	resultChannel := make(chan result, 1)
	go func() {
		token, err := readRegistryToken(reader)
		resultChannel <- result{token: token, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", errors.Join(ctx.Err(), reader.Close())
	case result := <-resultChannel:
		return result.token, result.err
	}
}

func validateRegistryToken(token string) error {
	if token == "" {
		return errors.New("registry token is empty")
	}
	return validatePrintableASCIIField("registry token", token, maximumRegistryTokenLength)
}

func loginMethodPicker() *tui.Picker {
	return tui.NewPicker("Choose login method", []tui.Item{
		{
			Label:       "Link",
			Description: "Open one-time authorization in a browser",
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

// browserLoginContext uses the same one-time device authorization as headless
// login, but opens GitHub's verification URL after displaying the short-lived
// user code. Long-lived credentials therefore never pass through a loopback URL.
func browserLoginContext(ctx context.Context, base string) string {
	return githubDeviceLoginContextUsing(ctx, base, true)
}

const (
	defaultDeviceFlowLifetime  = 15 * time.Minute
	maximumDeviceFlowLifetime  = 20 * time.Minute
	defaultDevicePollInterval  = 5 * time.Second
	maximumDevicePollInterval  = 60 * time.Second
	maximumOAuthClientIDLength = 256
	maximumOAuthScopeLength    = 128
	maximumDeviceCodeLength    = 1024
	maximumUserCodeLength      = 128
	maximumGitHubTokenLength   = 16 << 10
	maximumRegistryTokenLength = 64 << 10
	maximumRegistryInputLength = maximumRegistryTokenLength + 2 // optional CRLF
	maximumVerificationURLSize = 4 << 10
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
// exchanges the resulting GitHub token for a wago API token. Browser and
// headless modes both enter a code at github.com/login/device instead of relying
// on a localhost redirect; browser mode merely opens the verification URL.
//
// The registry advertises its GitHub OAuth client_id (GET /api/auth/github/client)
// so a self-hosted registry with its own OAuth app works without recompiling the
// CLI. The CLI talks to GitHub directly for the device + access token, then hands
// the GitHub token to the registry (POST /api/auth/github/exchange), which
// verifies the token belongs to its app and returns a wago token.
func githubDeviceLoginContext(ctx context.Context, base string) string {
	return githubDeviceLoginContextUsing(ctx, base, false)
}

func githubDeviceLoginContextUsing(ctx context.Context, base string, openBrowser bool) string {
	token, err := githubDeviceTokenContext(ctx, base, openBrowser)
	if err != nil {
		fatal("login: %v", err)
	}
	return token
}

type deviceFlowHooks struct {
	deviceCodeEndpoint  string
	accessTokenEndpoint string
	openBrowser         func(string) error
	wait                func(context.Context, time.Duration) error
}

func waitDevicePollContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func githubDeviceTokenContext(ctx context.Context, base string, openBrowser bool) (string, error) {
	return githubDeviceTokenUsingContext(ctx, base, openBrowser, deviceFlowHooks{
		deviceCodeEndpoint:  "https://github.com/login/device/code",
		accessTokenEndpoint: "https://github.com/login/oauth/access_token",
		openBrowser:         OpenBrowser,
		wait:                waitDevicePollContext,
	})
}

func githubDeviceTokenUsingContext(ctx context.Context, base string, openBrowser bool, hooks deviceFlowHooks) (string, error) {
	// 1. Ask the registry which GitHub OAuth app to authenticate against.
	status, data, err := apiRequestAtBaseLimitContext(ctx, base, http.MethodGet, "/api/auth/github/client", "", nil, registryAuthResponseLimit)
	if err != nil {
		return "", fmt.Errorf("fetching GitHub client config: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("fetching GitHub client config: %s", apiError(status, data))
	}
	var cfg struct {
		ClientID string `json:"client_id"`
		Scope    string `json:"scope"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parsing GitHub client config: %w", err)
	}
	if cfg.ClientID == "" {
		return "", errors.New("registry did not advertise a GitHub client id")
	}
	if err := validatePrintableASCIIField("GitHub client id", cfg.ClientID, maximumOAuthClientIDLength); err != nil {
		return "", err
	}
	cfg.Scope, err = allowedGitHubScopes(cfg.Scope)
	if err != nil {
		return "", err
	}

	// 2. Ask GitHub for a device + user code.
	var dc struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
		Error                   string `json:"error"`
	}
	if err := PostFormContext(ctx, hooks.deviceCodeEndpoint,
		url.Values{"client_id": {cfg.ClientID}, "scope": {cfg.Scope}}, &dc); err != nil {
		return "", fmt.Errorf("requesting device code from GitHub: %w", err)
	}
	if dc.Error != "" {
		if dc.Error == "device_flow_disabled" {
			return "", errors.New("GitHub device flow is disabled for this OAuth app — enable \"Device Flow\" in its settings")
		}
		return "", errors.New("GitHub rejected the device authorization request")
	}
	if dc.DeviceCode == "" || dc.UserCode == "" {
		return "", errors.New("GitHub returned an incomplete device authorization response")
	}
	if err := validatePrintableASCIIField("GitHub device code", dc.DeviceCode, maximumDeviceCodeLength); err != nil {
		return "", err
	}
	if err := validatePrintableASCIIField("GitHub user code", dc.UserCode, maximumUserCodeLength); err != nil {
		return "", err
	}

	lifetime, pollInterval := deviceFlowTiming(dc.ExpiresIn, dc.Interval)
	verifyURI := trustedDeviceVerificationURL(dc.VerificationURI, hooks.deviceCodeEndpoint)

	// Print the one-time code before launching a browser so it remains available
	// when GitHub does not provide a verification_uri_complete value.
	fmt.Printf("\n  First, copy your one-time code:\n\n      %s\n\n", bold(dc.UserCode))
	if openBrowser {
		browserURL := trustedDeviceVerificationURL(dc.VerificationURIComplete, hooks.deviceCodeEndpoint)
		if dc.VerificationURIComplete == "" || browserURL == "" {
			browserURL = verifyURI
		}
		if err := hooks.openBrowser(browserURL); err != nil {
			fmt.Printf("  Then open %s and enter it.\n", cyan(verifyURI))
		} else {
			fmt.Printf("%s Opened %s in your browser.\n", dim("→"), cyan(browserURL))
		}
	} else {
		fmt.Printf("  Then open %s and enter it.\n", cyan(verifyURI))
	}
	fmt.Printf("\n%s Waiting for you to authorize on GitHub…\n", dim("→"))

	// 3. Poll GitHub for the access token until the user authorizes or it expires.
	var ghToken string
	deadline := time.Now().Add(lifetime)
	for time.Now().Before(deadline) {
		wait := min(pollInterval, time.Until(deadline))
		if err := hooks.wait(ctx, wait); err != nil {
			return "", err
		}
		if !time.Now().Before(deadline) {
			break
		}
		var tr struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		if err := PostFormContext(ctx, hooks.accessTokenEndpoint, url.Values{
			"client_id":   {cfg.ClientID},
			"device_code": {dc.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &tr); err != nil {
			return "", fmt.Errorf("polling GitHub for the token: %w", err)
		}
		if tr.Error != "" {
			switch tr.Error {
			case "authorization_pending":
				// not authorized yet — keep polling
			case "slow_down":
				// RFC 8628 §3.5 asks for a five-second backoff. Keep the
				// server-controlled interval within the command's bounded policy.
				pollInterval = min(pollInterval+5*time.Second, maximumDevicePollInterval)
			case "access_denied":
				return "", errors.New("authorization was denied on GitHub")
			case "expired_token":
				return "", errors.New("the code expired before you authorized it — run `wago auth login --code` again")
			default:
				return "", errors.New("GitHub returned an unknown device authorization error")
			}
			continue
		}
		if tr.AccessToken != "" {
			if err := validatePrintableASCIIField("GitHub access token", tr.AccessToken, maximumGitHubTokenLength); err != nil {
				return "", err
			}
			ghToken = tr.AccessToken
			break
		}
		return "", errors.New("GitHub returned an incomplete device token response")
	}
	if ghToken == "" {
		return "", errors.New("timed out waiting for GitHub authorization")
	}

	// 4. Exchange the GitHub token for a wago API token.
	status, data, err = apiRequestAtBaseLimitContext(ctx, base, http.MethodPost, "/api/auth/github/exchange", "",
		map[string]string{"access_token": ghToken}, registryAuthResponseLimit)
	if err != nil {
		return "", fmt.Errorf("exchanging GitHub token: %w", err)
	}
	if status != http.StatusOK {
		// The exchange endpoint has received the GitHub access token. Do not include
		// its response body in an error in case a faulty registry reflects secrets.
		return "", fmt.Errorf("exchanging GitHub token: registry returned status %d", status)
	}
	var xr struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &xr); err != nil {
		return "", fmt.Errorf("parsing exchange response: %w", err)
	}
	if xr.Error != "" {
		return "", errors.New("registry returned an error during the GitHub exchange")
	}
	if xr.Token == "" {
		return "", errors.New("registry returned no token after the GitHub exchange")
	}
	if err := validateRegistryToken(xr.Token); err != nil {
		return "", err
	}
	return xr.Token, nil
}

func allowedGitHubScopes(scope string) (string, error) {
	if err := validateTerminalTextField("registry GitHub scope", scope, maximumOAuthScopeLength); err != nil {
		return "", err
	}
	requested := make(map[string]bool, 2)
	for _, item := range strings.Fields(scope) {
		switch item {
		case "read:user", "user:email":
			requested[item] = true
		default:
			return "", errors.New("registry requested an unsupported GitHub OAuth scope")
		}
	}
	ordered := make([]string, 0, len(requested))
	for _, item := range []string{"read:user", "user:email"} {
		if requested[item] {
			ordered = append(ordered, item)
		}
	}
	return strings.Join(ordered, " "), nil
}

func validatePrintableASCIIField(name, value string, maximum int) error {
	if len(value) > maximum {
		return fmt.Errorf("%s exceeds %d-byte limit", name, maximum)
	}
	for index := range len(value) {
		if value[index] < 0x21 || value[index] > 0x7e {
			return fmt.Errorf("%s contains non-printable characters", name)
		}
	}
	return nil
}

func trustedDeviceVerificationURL(candidate, deviceCodeEndpoint string) string {
	provider, err := url.Parse(deviceCodeEndpoint)
	if err != nil || provider.Scheme == "" || provider.Host == "" || provider.User != nil {
		return ""
	}
	fallback := &url.URL{Scheme: provider.Scheme, Host: provider.Host, Path: "/login/device"}
	if candidate == "" {
		return fallback.String()
	}
	if len(candidate) > maximumVerificationURLSize {
		return fallback.String()
	}
	target, err := url.Parse(candidate)
	if err != nil || target.Opaque != "" || target.User != nil || target.Fragment != "" ||
		!strings.EqualFold(target.Scheme, provider.Scheme) || !strings.EqualFold(target.Host, provider.Host) ||
		target.Path != "/login/device" {
		return fallback.String()
	}
	return target.String()
}

// registryLogout deletes stored credentials for the current registry.
func registryLogout() {
	registryLogoutContext(context.Background())
}

func registryLogoutContext(ctx context.Context) {
	base := registryBase()
	displayBase := registryDisplayBase(base)
	removed, err := deleteCredentialsResultContext(ctx, base)
	if err != nil {
		fatal("logout: %v", err)
	}
	if !removed {
		fmt.Printf("%s Not logged in to %s\n", dim("·"), displayBase)
		return
	}
	fmt.Printf("%s Logged out of %s\n", cyan("✓"), displayBase)
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

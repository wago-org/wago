//go:build !wago_lean

// Registry commands (login/logout/whoami/publish/unpublish/deprecate) for the
// wago registry at pkg.wago.sh. This file imports net/http (and net, os/exec for
// the browser login flow), so it is excluded from the size-optimized/TinyGo build
// (-tags wago_lean); that build gets the fatal() stubs in registry_stub.go.
//
// The credential store and URL helpers live in registry_config.go, which is
// net-free and shared by both builds.

package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// errUnauthorized marks a 401 from the registry (used to distinguish "not logged
// in" from a genuine transport/server error).
var errUnauthorized = errors.New("unauthorized")

// meResponse is the shape of GET /api/me.
type meResponse struct {
	ID     string `json:"id"`
	Login  string `json:"login"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Avatar string `json:"avatarUrl"`
}

// apiRequest performs an HTTP request to the current registry base with the
// bearer token (when non-empty) and an optional JSON body, returning the status
// code and raw response bytes.
func apiRequest(method, path, token string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, registryBase()+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// apiError extracts the {"error":...} message from a response body, falling back
// to the status code.
func apiError(status int, data []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return e.Error
	}
	return fmt.Sprintf("server returned status %d", status)
}

// fetchMe calls GET /api/me and returns the user, or errUnauthorized on a 401.
func fetchMe(token string) (meResponse, error) {
	status, data, err := apiRequest(http.MethodGet, "/api/me", token, nil)
	if err != nil {
		return meResponse{}, err
	}
	if status == http.StatusUnauthorized {
		return meResponse{}, errUnauthorized
	}
	if status != http.StatusOK {
		return meResponse{}, errors.New(apiError(status, data))
	}
	var me meResponse
	if err := json.Unmarshal(data, &me); err != nil {
		return meResponse{}, err
	}
	return me, nil
}

// registryLogin obtains an API token and stores it for the current registry.
// Interactively it asks whether to log in with a browser link (loopback flow) or
// a one-time code (device flow, for headless/remote machines); --link and --code
// pick one without prompting. --token <t> uses a token directly and --with-token
// reads one from stdin (for CI).
func registryLogin(c *Ctx) {
	withToken := c.Bool("with-token")
	code := c.Bool("code")
	link := c.Bool("link")
	token := c.Str("token")
	if code && link {
		fatal("login: choose either --code or --link, not both")
	}
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
		token = githubDeviceLogin(base)
	case link:
		token = browserLogin(base)
	default:
		token = chooseLoginMethod(base)
	}
	me, err := fetchMe(token)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			fatal("login: the registry rejected that token")
		}
		fatal("login: %v", err)
	}
	if err := saveCredentials(base, token, me.Login); err != nil {
		fatal("login: saving credentials: %v", err)
	}
	fmt.Printf("%s Logged in as %s\n", cyan("✓"), bold(me.Login))
}

// chooseLoginMethod asks the user how to log in: a browser link on this machine
// (loopback flow) or a one-time code entered on any device (device flow, for
// remote/headless machines). It defaults to the browser link when there is no
// answer (e.g. a non-interactive stdin).
func chooseLoginMethod(base string) string {
	fmt.Printf("How would you like to log in?\n\n")
	fmt.Printf("  %s  open a browser link on this machine\n", bold("1) link"))
	fmt.Printf("  %s  enter a one-time code at github.com/login/device (for remote/headless)\n", bold("2) code"))
	fmt.Printf("\nChoose [1/2] (default 1): ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "2", "code", "c":
		return githubDeviceLogin(base)
	default:
		return browserLogin(base)
	}
}

// browserLogin runs the loopback OAuth flow: it listens on a free localhost port,
// opens the browser to the registry's CLI-login endpoint, and waits for the
// /callback redirect carrying the plaintext token. It fatals on error or timeout.
func browserLogin(base string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal("login: cannot open a loopback listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	state, err := randomState()
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
		_, _ = io.WriteString(w, loginSuccessHTML)
		resCh <- result{token: q.Get("token")}
	})
	go srv.Serve(ln)
	defer srv.Close()

	loginURL := fmt.Sprintf("%s/auth/cli/login?port=%d&state=%s", base, port, url.QueryEscape(state))
	if err := openBrowser(loginURL); err != nil {
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
	case <-time.After(2 * time.Minute):
		fatal("login: timed out waiting for the browser callback")
		return ""
	}
}

// githubDeviceLogin runs GitHub's OAuth device flow (RFC 8628) and exchanges the
// resulting GitHub token for a wago API token. It's the login path for
// headless/remote machines: the user enters a code at github.com/login/device
// instead of relying on a localhost redirect.
//
// The registry advertises its GitHub OAuth client_id (GET /api/auth/github/client)
// so a self-hosted registry with its own OAuth app works without recompiling the
// CLI. The CLI talks to GitHub directly for the device + access token, then hands
// the GitHub token to the registry (POST /api/auth/github/exchange), which
// verifies the token belongs to its app and returns a wago token.
func githubDeviceLogin(base string) string {
	// 1. Ask the registry which GitHub OAuth app to authenticate against.
	status, data, err := apiRequest(http.MethodGet, "/api/auth/github/client", "", nil)
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
	if err := githubForm("https://github.com/login/device/code",
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

	interval := dc.Interval
	if interval <= 0 {
		interval = 5 // RFC 8628 §3.5 default
	}
	expiresIn := dc.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 15 * 60
	}
	verifyURI := dc.VerificationURI
	if verifyURI == "" {
		verifyURI = "https://github.com/login/device"
	}

	fmt.Printf("\n  First, copy your one-time code:\n\n      %s\n\n", bold(dc.UserCode))
	fmt.Printf("  Then open %s and enter it.\n", cyan(verifyURI))
	if err := openBrowser(verifyURI); err == nil {
		fmt.Printf("  %s\n", dim("(we also tried to open your browser)"))
	}
	fmt.Printf("\n%s Waiting for you to authorize on GitHub…\n", dim("→"))

	// 3. Poll GitHub for the access token until the user authorizes or it expires.
	var ghToken string
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)
		var tr struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		if err := githubForm("https://github.com/login/oauth/access_token", url.Values{
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
			interval += 5 // RFC 8628 §3.5: back off by 5s on slow_down
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
	status, data, err = apiRequest(http.MethodPost, "/api/auth/github/exchange", "",
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

// githubForm POSTs a form-encoded request to a github.com endpoint and decodes
// its JSON reply into out.
func githubForm(endpoint string, form url.Values, out any) error {
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// loginSuccessHTML is the page shown in the browser after a successful login.
const loginSuccessHTML = `<!doctype html><html><head><meta charset="utf-8">` +
	`<title>wago — logged in</title></head>` +
	`<body style="font-family:system-ui,sans-serif;text-align:center;padding:4rem">` +
	`<h1>You're logged in ✓</h1><p>You can close this tab and return to your terminal.</p>` +
	`</body></html>`

// openBrowser opens url in the user's default browser (platform-specific).
func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

// randomState returns a random hex string for the login CSRF state parameter.
func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// registryLogout deletes stored credentials for the current registry.
func registryLogout(_ *Ctx) {
	base := registryBase()
	creds, _ := loadCredentials()
	if _, ok := creds[base]; !ok {
		fmt.Printf("%s Not logged in to %s\n", dim("·"), base)
		return
	}
	if err := deleteCredentials(base); err != nil {
		fatal("logout: %v", err)
	}
	fmt.Printf("%s Logged out of %s\n", cyan("✓"), base)
}

// registryWhoami prints the login of the current token, or a friendly hint when
// there is no valid session.
func registryWhoami(_ *Ctx) {
	token := resolveToken()
	if token == "" {
		fmt.Println("not logged in (run: wago auth login)")
		return
	}
	me, err := fetchMe(token)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			fmt.Println("not logged in (run: wago auth login)")
			return
		}
		fatal("whoami: %v", err)
	}
	fmt.Println(me.Login)
}

// registryPublish reads a wago-plugin.json manifest and POSTs it to /api/publish
// along with a version, commit, and optional metadata.
func registryPublish(c *Ctx) {
	manifestPath := c.Str("manifest")
	ver := c.Str("version")
	commit := c.Str("commit")
	notes := c.Str("notes")
	category := c.Str("category")
	tags := c.Str("tags")
	token := resolveToken()
	if token == "" {
		fatal("publish: not logged in (run: wago auth login)")
	}
	if manifestPath == "" {
		manifestPath = "wago-plugin.json"
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		fatal("publish: reading manifest: %v", err)
	}
	var mf struct {
		Schema string `json:"schema"`
		Module string `json:"module"`
	}
	if err := json.Unmarshal(raw, &mf); err != nil {
		fatal("publish: parsing %s: %v", manifestPath, err)
	}
	if strings.TrimSpace(mf.Module) == "" {
		fatal("publish: %s has no \"module\" field", manifestPath)
	}

	if ver == "" {
		ver = strings.TrimSpace(gitOutput("describe", "--tags", "--abbrev=0"))
		if ver == "" {
			fatal("publish: no --version given and `git describe --tags` found no tag; pass --version <v>")
		}
	}
	if commit == "" {
		commit = strings.TrimSpace(gitOutput("rev-parse", "HEAD")) // best-effort; "" if not a repo
	}

	body := map[string]any{
		"manifest": json.RawMessage(raw),
		"version":  ver,
		"commit":   commit,
		"notes":    notes,
		"category": category,
	}
	if t := splitCommaList(tags); len(t) > 0 {
		body["tags"] = t
	}

	status, data, err := apiRequest(http.MethodPost, "/api/publish", token, body)
	if err != nil {
		fatal("publish: %v", err)
	}
	switch status {
	case http.StatusOK:
		fmt.Printf("%s Published %s %s\n", cyan("✓"), bold(mf.Module), ver)
		fmt.Printf("  %s\n", dim(frontendBase()+"/#/p/"+shortFromModule(mf.Module)))
	case http.StatusConflict:
		fatal("publish: version %s is already published", ver)
	case http.StatusForbidden:
		fatal("publish: you are not the owner of %s", mf.Module)
	case http.StatusUnauthorized:
		fatal("publish: not logged in (run: wago auth login)")
	default:
		fatal("publish: %s", apiError(status, data))
	}
}

// registryUnpublish removes a whole package, or a single version when the
// argument carries an @version suffix. It confirms first unless --yes is given.
func registryUnpublish(c *Ctx) {
	yes := c.Bool("yes")
	pos := c.Args
	if len(pos) != 1 {
		fatal("unpublish: need <module-or-short>[@version]")
	}
	token := resolveToken()
	if token == "" {
		fatal("unpublish: not logged in (run: wago auth login)")
	}
	name, ver := splitAtVersion(pos[0])
	target := name
	if ver != "" {
		target = name + "@" + ver
	}
	if !yes && !confirm(fmt.Sprintf("Unpublish %s? This cannot be undone.", target)) {
		fmt.Println("aborted")
		return
	}

	path := "/api/packages/" + url.PathEscape(name)
	if ver != "" {
		path += "/versions/" + url.PathEscape(ver)
	}
	status, data, err := apiRequest(http.MethodDelete, path, token, nil)
	if err != nil {
		fatal("unpublish: %v", err)
	}
	switch status {
	case http.StatusOK:
		fmt.Printf("%s Unpublished %s\n", cyan("✓"), target)
	case http.StatusForbidden:
		fatal("unpublish: you are not the owner of %s", name)
	case http.StatusNotFound:
		fatal("unpublish: %s not found", target)
	case http.StatusUnauthorized:
		fatal("unpublish: not logged in (run: wago auth login)")
	default:
		fatal("unpublish: %s", apiError(status, data))
	}
}

// registryDeprecate marks a package (or a specific @version) deprecated, or
// reverses it with --undo. --message sets the deprecation notice.
func registryDeprecate(c *Ctx) {
	undo := c.Bool("undo")
	message := c.Str("message")
	pos := c.Args
	if len(pos) != 1 {
		fatal("deprecate: need <module-or-short>[@version]")
	}
	token := resolveToken()
	if token == "" {
		fatal("deprecate: not logged in (run: wago auth login)")
	}
	name, ver := splitAtVersion(pos[0])
	target := name
	if ver != "" {
		target = name + "@" + ver
	}

	body := map[string]any{"message": message, "version": ver, "undo": undo}
	path := "/api/packages/" + url.PathEscape(name) + "/deprecate"
	status, data, err := apiRequest(http.MethodPost, path, token, body)
	if err != nil {
		fatal("deprecate: %v", err)
	}
	switch status {
	case http.StatusOK:
		if undo {
			fmt.Printf("%s Un-deprecated %s\n", cyan("✓"), target)
		} else {
			fmt.Printf("%s Deprecated %s\n", cyan("✓"), target)
		}
	case http.StatusForbidden:
		fatal("deprecate: you are not the owner of %s", name)
	case http.StatusNotFound:
		fatal("deprecate: %s not found", target)
	case http.StatusUnauthorized:
		fatal("deprecate: not logged in (run: wago auth login)")
	default:
		fatal("deprecate: %s", apiError(status, data))
	}
}

// gitOutput runs `git <args...>` and returns stdout, or "" on any error (so
// callers can treat git as best-effort).
func gitOutput(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// confirm prompts on stderr and reads a yes/no answer from stdin (default no).
func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// splitAtVersion splits "name@version" into its parts; the version is "" when
// there is no @ (module paths never contain @).
func splitAtVersion(s string) (name, version string) {
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// splitCommaList splits a comma-separated string into trimmed, non-empty items.
func splitCommaList(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

//go:build !wago_lean

// Registry commands (login/logout/whoami/search/info/versions/star/unstar/token/
// publish/unpublish/deprecate) for the wago registry at pkg.wago.sh. This file
// imports net/http (and net, os/exec for the browser login flow), so it is
// excluded from the size-optimized/TinyGo build
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
	"strconv"
	"strings"
	"text/tabwriter"
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
// Default is an interactive loopback browser flow. --device runs the OAuth device
// flow (RFC 8628) for a headless or remote machine where the localhost redirect
// can't reach the CLI. --token <t> uses a token directly and --with-token reads
// one from stdin (for CI).
func registryLogin(args []string) {
	withToken, args := hasFlag(args, "--with-token")
	device, args := hasFlag(args, "--device")
	var token string
	if _, err := extractOpts(args, map[string]*string{"--token": &token}); err != nil {
		fatal("login: %v", err)
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
	case device:
		token = deviceLogin(base)
	default:
		token = browserLogin(base)
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

// deviceCodeResponse is the registry's reply to POST /api/device/code (RFC 8628
// device authorization). verification_uri_complete embeds the user code so a
// browser can skip the manual entry; it is optional.
type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"` // seconds until the code expires
	Interval                int    `json:"interval"`   // seconds to wait between polls
}

// deviceLogin runs the OAuth 2.0 device authorization grant (RFC 8628) for a
// machine with no reachable localhost redirect: it asks the registry for a device
// + user code, shows the user a URL and code to enter on any device, then polls
// the token endpoint until the request is approved, denied, or expires.
//
// Server contract:
//   - POST /api/device/code            → 200 deviceCodeResponse
//   - POST /api/device/token {device_code} → 200 {"token": "..."} once approved,
//     else a 4xx with {"error": "..."} carrying an RFC 8628 status:
//     "authorization_pending", "slow_down", "access_denied", or "expired_token".
func deviceLogin(base string) string {
	status, data, err := apiRequest(http.MethodPost, "/api/device/code", "", struct{}{})
	if err != nil {
		fatal("login: requesting device code: %v", err)
	}
	if status != http.StatusOK {
		fatal("login: requesting device code: %s", apiError(status, data))
	}
	var dc deviceCodeResponse
	if err := json.Unmarshal(data, &dc); err != nil {
		fatal("login: parsing device authorization response: %v", err)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		fatal("login: registry returned an incomplete device authorization response")
	}

	interval := dc.Interval
	if interval <= 0 {
		interval = 5 // RFC 8628 §3.5 default
	}
	expiresIn := dc.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 15 * 60
	}

	fmt.Printf("\n  First, copy your one-time code:\n\n      %s\n\n", bold(dc.UserCode))
	fmt.Printf("  Then open %s on any device and enter it.\n", cyan(dc.VerificationURI))
	if dc.VerificationURIComplete != "" {
		if err := openBrowser(dc.VerificationURIComplete); err == nil {
			fmt.Printf("  %s\n", dim("(we also tried to open your browser)"))
		}
	}
	fmt.Printf("\n%s Waiting for approval…\n", dim("→"))

	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)
		status, data, err := apiRequest(http.MethodPost, "/api/device/token", "", map[string]string{"device_code": dc.DeviceCode})
		if err != nil {
			fatal("login: %v", err)
		}
		if status == http.StatusOK {
			var tr struct {
				Token string `json:"token"`
			}
			if err := json.Unmarshal(data, &tr); err != nil {
				fatal("login: parsing token response: %v", err)
			}
			if tr.Token == "" {
				fatal("login: registry approved the request but returned no token")
			}
			return tr.Token
		}
		switch apiError(status, data) {
		case "authorization_pending":
			// not approved yet — keep polling
		case "slow_down":
			interval += 5 // RFC 8628 §3.5: back off by 5s on slow_down
		case "access_denied":
			fatal("login: request was denied")
		case "expired_token":
			fatal("login: the code expired before it was approved — run `wago login --device` again")
		default:
			fatal("login: %s", apiError(status, data))
		}
	}
	fatal("login: timed out waiting for approval")
	return ""
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
func registryLogout(args []string) {
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
func registryWhoami(args []string) {
	token := resolveToken()
	if token == "" {
		fmt.Println("not logged in (run: wago login)")
		return
	}
	me, err := fetchMe(token)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			fmt.Println("not logged in (run: wago login)")
			return
		}
		fatal("whoami: %v", err)
	}
	fmt.Println(me.Login)
}

// registryPublish reads a wago-plugin.json manifest and POSTs it to /api/publish
// along with a version, commit, and optional metadata.
func registryPublish(args []string) {
	var manifestPath, ver, commit, notes, category, tags string
	if _, err := extractOpts(args, map[string]*string{
		"--manifest": &manifestPath,
		"--version":  &ver,
		"--commit":   &commit,
		"--notes":    &notes,
		"--category": &category,
		"--tags":     &tags,
	}); err != nil {
		fatal("publish: %v", err)
	}
	token := resolveToken()
	if token == "" {
		fatal("publish: not logged in (run: wago login)")
	}
	if manifestPath == "" {
		manifestPath = "wago-plugin.json"
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		fatal("publish: reading manifest: %v", err)
	}
	var mf struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &mf); err != nil {
		fatal("publish: parsing %s: %v", manifestPath, err)
	}
	if strings.TrimSpace(mf.Name) == "" {
		fatal("publish: %s has no \"name\" field", manifestPath)
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
		fmt.Printf("%s Published %s %s\n", cyan("✓"), bold(mf.Name), ver)
		fmt.Printf("  %s\n", dim(frontendBase()+"/#/p/"+shortFromModule(mf.Name)))
	case http.StatusConflict:
		fatal("publish: version %s is already published", ver)
	case http.StatusForbidden:
		fatal("publish: you are not the owner of %s", mf.Name)
	case http.StatusUnauthorized:
		fatal("publish: not logged in (run: wago login)")
	default:
		fatal("publish: %s", apiError(status, data))
	}
}

// registryUnpublish removes a whole package, or a single version when the
// argument carries an @version suffix. It confirms first unless --yes is given.
func registryUnpublish(args []string) {
	yes, args := hasFlag(args, "--yes")
	pos, err := extractOpts(args, map[string]*string{})
	if err != nil {
		fatal("unpublish: %v", err)
	}
	if len(pos) != 1 {
		fatal("unpublish: need <module-or-short>[@version]")
	}
	token := resolveToken()
	if token == "" {
		fatal("unpublish: not logged in (run: wago login)")
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
		fatal("unpublish: not logged in (run: wago login)")
	default:
		fatal("unpublish: %s", apiError(status, data))
	}
}

// registryDeprecate marks a package (or a specific @version) deprecated, or
// reverses it with --undo. --message sets the deprecation notice.
func registryDeprecate(args []string) {
	undo, args := hasFlag(args, "--undo")
	var message string
	pos, err := extractOpts(args, map[string]*string{"--message": &message})
	if err != nil {
		fatal("deprecate: %v", err)
	}
	if len(pos) != 1 {
		fatal("deprecate: need <module-or-short>[@version]")
	}
	token := resolveToken()
	if token == "" {
		fatal("deprecate: not logged in (run: wago login)")
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
		fatal("deprecate: not logged in (run: wago login)")
	default:
		fatal("deprecate: %s", apiError(status, data))
	}
}

// ---- read endpoints (no auth) -------------------------------------------
//
// search/info/versions hit the public read API and need no token. Reviews,
// comments, votes, and install recording are website/UI features and are
// deliberately not exposed here.

// pkg is the shape of a package as returned by the read endpoints. It covers
// both the list rows from /api/packages and the fuller single-package document
// from /api/packages/{name}; fields absent from one form stay zero-valued.
type pkg struct {
	Short             string   `json:"short"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Version           string   `json:"version"`
	License           string   `json:"license"`
	Stability         string   `json:"stability"`
	Rating            float64  `json:"rating"`
	RatingCount       int      `json:"ratingCount"`
	Stars             int      `json:"stars"`
	InstallsWeekLabel string   `json:"installsWeekLabel"`
	Repository        string   `json:"repository"`
	Verified          bool     `json:"verified"`
	DeprecatedMessage string   `json:"deprecatedMessage"`
	Subpackages       []subpkg `json:"subpackages"`
	Compatibility     struct {
		Engines   map[string]string `json:"engines"`
		Platforms []string          `json:"platforms"`
	} `json:"compatibility"`
}

// subpkg is one importable sub-package within a package.
type subpkg struct {
	ID        string `json:"id"`
	Import    string `json:"import"`
	Version   string `json:"version"`
	Stability string `json:"stability"`
}

// registrySearch queries /api/packages and prints an aligned table of matches
// (or the raw JSON with --json). No auth. --limit caps the rows shown.
func registrySearch(args []string) {
	var limit string
	jsonOut, args := hasFlag(args, "--json")
	pos, err := extractOpts(args, map[string]*string{"--limit": &limit})
	if err != nil {
		fatal("search: %v", err)
	}
	if len(pos) == 0 {
		fatal("search: need a <query>")
	}
	max := 20
	if limit != "" {
		if n, err := strconv.Atoi(limit); err != nil || n < 1 {
			fatal("search: --limit wants a positive integer, got %q", limit)
		} else {
			max = n
		}
	}
	q := url.Values{}
	q.Set("q", strings.Join(pos, " "))
	status, data, err := apiRequest(http.MethodGet, "/api/packages?"+q.Encode(), "", nil)
	if err != nil {
		fatal("search: %v", err)
	}
	if status != http.StatusOK {
		fatal("search: %s", apiError(status, data))
	}
	if jsonOut {
		os.Stdout.Write(data)
		return
	}
	var resp struct {
		Packages []pkg `json:"packages"`
		Total    int   `json:"total"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		fatal("search: decoding response: %v", err)
	}
	if len(resp.Packages) == 0 {
		fmt.Printf("%s no packages match %q\n", dim("·"), strings.Join(pos, " "))
		return
	}
	rows := resp.Packages
	if len(rows) > max {
		rows = rows[:max]
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, p := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			bold(p.Short),
			dim(p.Version),
			ratingStar(p.Rating),
			dim("↓"+installsLabel(p.InstallsWeekLabel)+"/wk"),
			truncate(p.Description, 50))
	}
	w.Flush()
	if len(resp.Packages) > len(rows) {
		fmt.Printf("%s\n", dim(fmt.Sprintf("… %d more of %d (raise --limit to see more)", len(resp.Packages)-len(rows), resp.Total)))
	}
}

// registryInfo prints a single package's details from /api/packages/{name}, or
// the raw JSON with --json. No auth. A 404 is reported as "no such package".
func registryInfo(args []string) {
	jsonOut, pos := hasFlag(args, "--json")
	if len(pos) != 1 {
		fatal("info: need exactly one <pkg>")
	}
	status, data, err := apiRequest(http.MethodGet, "/api/packages/"+url.PathEscape(pos[0]), "", nil)
	if err != nil {
		fatal("info: %v", err)
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		fatal("info: no such package %q", pos[0])
	default:
		fatal("info: %s", apiError(status, data))
	}
	if jsonOut {
		os.Stdout.Write(data)
		return
	}
	var p pkg
	if err := json.Unmarshal(data, &p); err != nil {
		fatal("info: decoding response: %v", err)
	}
	fmt.Printf("%s", bold(p.Name))
	if p.Verified {
		fmt.Printf(" %s", cyan("✓"))
	}
	fmt.Println()
	if p.Description != "" {
		fmt.Printf("  %s\n", p.Description)
	}
	if p.DeprecatedMessage != "" {
		fmt.Printf("  %s\n", red("⚠ deprecated: "+p.DeprecatedMessage))
	}
	fmt.Println()
	rel := p.Version
	if p.Stability != "" {
		rel += " · " + p.Stability
	}
	if p.License != "" {
		rel += " · " + p.License
	}
	printField("version", rel)
	printField("rating", fmt.Sprintf("%.1f/5 from %d  %s%d", p.Rating, p.RatingCount, "★", p.Stars))
	printField("installs", installsLabel(p.InstallsWeekLabel)+"/wk")
	if p.Repository != "" {
		printField("repo", p.Repository)
	}
	if eng := engineConstraints(p.Compatibility.Engines); eng != "" {
		printField("engines", eng)
	}
	if len(p.Compatibility.Platforms) > 0 {
		printField("platforms", strings.Join(p.Compatibility.Platforms, ", "))
	}
	if len(p.Subpackages) > 0 {
		fmt.Printf("\n%s\n", bold("subpackages:"))
		for _, s := range p.Subpackages {
			imp := s.Import
			if imp == "" {
				imp = s.ID
			}
			fmt.Printf("  %s %s %s\n", cyan(s.ID), dim("←"), imp)
		}
	}
}

// registryVersions lists a package's published versions from
// /api/packages/{name}/versions, or the raw JSON with --json. No auth.
func registryVersions(args []string) {
	jsonOut, pos := hasFlag(args, "--json")
	if len(pos) != 1 {
		fatal("versions: need exactly one <pkg>")
	}
	status, data, err := apiRequest(http.MethodGet, "/api/packages/"+url.PathEscape(pos[0])+"/versions", "", nil)
	if err != nil {
		fatal("versions: %v", err)
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		fatal("versions: no such package %q", pos[0])
	default:
		fatal("versions: %s", apiError(status, data))
	}
	if jsonOut {
		os.Stdout.Write(data)
		return
	}
	var resp struct {
		Versions []struct {
			Version     string `json:"version"`
			Commit      string `json:"commit"`
			PublishedAt string `json:"publishedAt"`
			Notes       string `json:"notes"`
			Latest      bool   `json:"latest"`
			Deprecated  bool   `json:"deprecated"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		fatal("versions: decoding response: %v", err)
	}
	if len(resp.Versions) == 0 {
		fmt.Printf("%s no published versions\n", dim("·"))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, v := range resp.Versions {
		var tags string
		if v.Latest {
			tags += " " + cyan("latest")
		}
		if v.Deprecated {
			tags += " " + red("deprecated")
		}
		fmt.Fprintf(w, "%s\t%s\t%s%s\t%s\n",
			bold(v.Version),
			dim(shortCommit(v.Commit)),
			dim(v.PublishedAt), tags,
			truncate(v.Notes, 50))
	}
	w.Flush()
}

// ---- star / unstar (auth) -----------------------------------------------

// registryStar stars a package via POST /api/packages/{name}/star.
func registryStar(args []string) { setStar(args, http.MethodPost, "star") }

// registryUnstar removes a star via DELETE /api/packages/{name}/star.
func registryUnstar(args []string) { setStar(args, http.MethodDelete, "unstar") }

// setStar toggles the caller's star on a package and prints the new count. The
// verb is only used to shape command name and messages.
func setStar(args []string, method, verb string) {
	pos, err := extractOpts(args, map[string]*string{})
	if err != nil {
		fatal("%s: %v", verb, err)
	}
	if len(pos) != 1 {
		fatal("%s: need exactly one <pkg>", verb)
	}
	token := resolveToken()
	if token == "" {
		fatal("%s: not logged in (run: wago login)", verb)
	}
	status, data, err := apiRequest(method, "/api/packages/"+url.PathEscape(pos[0])+"/star", token, nil)
	if err != nil {
		fatal("%s: %v", verb, err)
	}
	switch status {
	case http.StatusOK:
	case http.StatusUnauthorized:
		fatal("%s: not logged in (run: wago login)", verb)
	case http.StatusNotFound:
		fatal("%s: no such package %q", verb, pos[0])
	default:
		fatal("%s: %s", verb, apiError(status, data))
	}
	var resp struct {
		Stars   int  `json:"stars"`
		Starred bool `json:"starred"`
	}
	_ = json.Unmarshal(data, &resp)
	if verb == "star" {
		fmt.Printf("%s Starred %s (%d)\n", cyan("★"), bold(pos[0]), resp.Stars)
	} else {
		fmt.Printf("%s Unstarred %s (%d)\n", dim("·"), bold(pos[0]), resp.Stars)
	}
}

// ---- tokens (auth) ------------------------------------------------------

// registryToken dispatches the token sub-commands: list, create, revoke.
func registryToken(args []string) {
	if len(args) == 0 {
		fatal("token: need a sub-command (list, create, revoke)")
	}
	switch args[0] {
	case "list", "ls":
		registryTokenList(args[1:])
	case "create", "new":
		registryTokenCreate(args[1:])
	case "revoke", "rm", "delete":
		registryTokenRevoke(args[1:])
	default:
		fatal("token: unknown sub-command %q (want: list, create, revoke)", args[0])
	}
}

// registryTokenList prints the caller's API tokens from GET /api/tokens.
func registryTokenList(args []string) {
	token := resolveToken()
	if token == "" {
		fatal("token list: not logged in (run: wago login)")
	}
	status, data, err := apiRequest(http.MethodGet, "/api/tokens", token, nil)
	if err != nil {
		fatal("token list: %v", err)
	}
	switch status {
	case http.StatusOK:
	case http.StatusUnauthorized:
		fatal("token list: not logged in (run: wago login)")
	default:
		fatal("token list: %s", apiError(status, data))
	}
	var resp struct {
		Tokens []struct {
			ID         string `json:"id"`
			Label      string `json:"label"`
			CreatedAt  string `json:"createdAt"`
			LastUsedAt string `json:"lastUsedAt"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		fatal("token list: decoding response: %v", err)
	}
	if len(resp.Tokens) == 0 {
		fmt.Printf("%s no tokens (create one: wago token create)\n", dim("·"))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", dim("ID"), dim("LABEL"), dim("CREATED"), dim("LAST USED"))
	for _, t := range resp.Tokens {
		label := t.Label
		if label == "" {
			label = dim("(none)")
		}
		last := t.LastUsedAt
		if last == "" {
			last = dim("never")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", bold(t.ID), label, t.CreatedAt, last)
	}
	w.Flush()
}

// registryTokenCreate mints a new API token via POST /api/tokens and prints the
// plaintext value once — it cannot be retrieved again.
func registryTokenCreate(args []string) {
	var label string
	if _, err := extractOpts(args, map[string]*string{"--label": &label}); err != nil {
		fatal("token create: %v", err)
	}
	token := resolveToken()
	if token == "" {
		fatal("token create: not logged in (run: wago login)")
	}
	body := map[string]any{}
	if label != "" {
		body["label"] = label
	}
	status, data, err := apiRequest(http.MethodPost, "/api/tokens", token, body)
	if err != nil {
		fatal("token create: %v", err)
	}
	switch status {
	case http.StatusOK, http.StatusCreated:
	case http.StatusUnauthorized:
		fatal("token create: not logged in (run: wago login)")
	default:
		fatal("token create: %s", apiError(status, data))
	}
	var resp struct {
		Token string `json:"token"`
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		fatal("token create: decoding response: %v", err)
	}
	fmt.Printf("%s Created token %s\n", cyan("✓"), bold(resp.ID))
	fmt.Printf("\n  %s\n\n", bold(resp.Token))
	fmt.Printf("%s Save this now — it will not be shown again.\n", red("!"))
}

// registryTokenRevoke deletes an API token by id via DELETE /api/tokens/{id}.
func registryTokenRevoke(args []string) {
	pos, err := extractOpts(args, map[string]*string{})
	if err != nil {
		fatal("token revoke: %v", err)
	}
	if len(pos) != 1 {
		fatal("token revoke: need exactly one <id>")
	}
	token := resolveToken()
	if token == "" {
		fatal("token revoke: not logged in (run: wago login)")
	}
	status, data, err := apiRequest(http.MethodDelete, "/api/tokens/"+url.PathEscape(pos[0]), token, nil)
	if err != nil {
		fatal("token revoke: %v", err)
	}
	switch status {
	case http.StatusOK, http.StatusNoContent:
		fmt.Printf("%s Revoked token %s\n", cyan("✓"), bold(pos[0]))
	case http.StatusUnauthorized:
		fatal("token revoke: not logged in (run: wago login)")
	case http.StatusNotFound:
		fatal("token revoke: no such token %q", pos[0])
	default:
		fatal("token revoke: %s", apiError(status, data))
	}
}

// ---- small formatting helpers -------------------------------------------

// printField prints one aligned "label value" line for the info view.
func printField(label, value string) {
	fmt.Printf("  %s %s\n", dim(fmt.Sprintf("%-9s", label)), value)
}

// ratingStar renders a rating as a "★x.x" badge (blank when unrated).
func ratingStar(r float64) string {
	if r <= 0 {
		return dim("★ —")
	}
	return fmt.Sprintf("★%.1f", r)
}

// installsLabel returns the weekly-installs label, or "0" when empty.
func installsLabel(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// engineConstraints renders the engines map (wago/tinygo/go) as a compact,
// stably-ordered string.
func engineConstraints(engines map[string]string) string {
	var parts []string
	for _, k := range []string{"wago", "tinygo", "go"} {
		if v := engines[k]; v != "" {
			parts = append(parts, k+" "+v)
		}
	}
	return strings.Join(parts, ", ")
}

// shortCommit trims a git SHA to its first 7 chars.
func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
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

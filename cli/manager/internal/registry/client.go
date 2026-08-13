package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/internal/httpclient"
)

const registryResponseLimit int64 = 4 << 20

const (
	registryAuthResponseLimit       int64 = 128 << 10
	maximumRegistryLoginLength            = 256
	maximumRegistryDisplayURLLength       = 4 << 10
)

var (
	registryResponseMaximum = registryResponseLimit
	registryHTTP            = httpclient.NewAPI()
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
	return apiRequestContext(context.Background(), method, path, token, body)
}

func apiRequestContext(ctx context.Context, method, path, token string, body any) (int, []byte, error) {
	return apiRequestAtBaseContext(ctx, registryBase(), method, path, token, body)
}

func apiRequestAtBaseContext(ctx context.Context, base, method, path, token string, body any) (int, []byte, error) {
	return apiRequestAtBaseLimitContext(ctx, base, method, path, token, body, registryResponseMaximum)
}

func apiRequestAtBaseLimitContext(ctx context.Context, base, method, path, token string, body any, responseLimit int64) (int, []byte, error) {
	if err := automation.RequireOnline("registry request"); err != nil {
		return 0, nil, err
	}
	if err := validateRegistryBaseURL(base); err != nil {
		return 0, nil, err
	}
	if token != "" {
		if err := validateRegistryToken(token); err != nil {
			return 0, nil, err
		}
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := registryHTTP.Bytes(ctx, req, min(responseLimit, registryResponseMaximum))
	if token != "" && (response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices) {
		// A credential-bearing endpoint may accidentally reflect the bearer or
		// request body in an error. Preserve only the status across this trust
		// boundary so callers cannot render reflected credentials.
		response.Body = nil
	}
	return response.StatusCode, response.Body, err
}

func validateRegistryBaseURL(base string) error {
	parsed, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("invalid registry URL: %w", err)
	}
	if parsed.Opaque != "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("invalid registry URL: expected an HTTP(S) base URL without credentials, query, or fragment")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return errors.New("insecure registry URL: HTTPS is required except for loopback development servers")
	default:
		return errors.New("invalid registry URL: expected HTTPS or loopback HTTP")
	}
}

// ValidateBaseURL applies the registry transport policy to consumers that use
// the configured registry outside the JSON API helper, such as catalog lookup.
func ValidateBaseURL(base string) error { return validateRegistryBaseURL(base) }

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// recordRegistryInstall reports one successfully installed plugin to the public
// registry. Metrics must never make `wago add` fail, and the short timeout keeps
// full-module installs usable when the registry is offline.
func recordRegistryInstall(module, version string) {
	recordRegistryInstallContext(context.Background(), module, version)
}

func recordRegistryInstallContext(parent context.Context, module, version string) {
	if automation.Offline() {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	path := "/api/packages/" + url.PathEscape(module) + "/installs"
	_, _, _ = apiRequestAtBaseLimitContext(ctx, registryBase(), http.MethodPost, path, "",
		map[string]string{"version": version}, 4<<10)
}

// apiError extracts the {"error":...} message from a response body, falling back
// to the status code.
func apiError(status int, data []byte) string {
	return ResponseError(status, data)
}

// ResponseError returns a bounded, terminal-safe registry error or a numeric
// status fallback. It is shared by registry consumers outside this package.
func ResponseError(status int, data []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" && validateTerminalTextField("registry error", e.Error, 1024) == nil {
		return e.Error
	}
	return fmt.Sprintf("server returned status %d", status)
}

func registryDisplayBase(base string) string {
	if validateRegistryBaseURL(base) != nil ||
		validateTerminalTextField("registry URL", base, maximumRegistryDisplayURLLength) != nil {
		return "configured registry"
	}
	return base
}

func validateTerminalTextField(name, value string, maximum int) error {
	if len(value) > maximum {
		return fmt.Errorf("%s exceeds %d-byte limit", name, maximum)
	}
	for index := range len(value) {
		if value[index] < 0x20 || value[index] > 0x7e {
			return fmt.Errorf("%s contains non-printable characters", name)
		}
	}
	return nil
}

// fetchMe calls GET /api/me and returns the user, or errUnauthorized on a 401.
func fetchMe(token string) (meResponse, error) {
	return fetchMeContext(context.Background(), token)
}

func fetchMeContext(ctx context.Context, token string) (meResponse, error) {
	return fetchMeAtBaseContext(ctx, registryBase(), token)
}

func fetchMeAtBaseContext(ctx context.Context, base, token string) (meResponse, error) {
	status, data, err := apiRequestAtBaseLimitContext(ctx, base, http.MethodGet, "/api/me", token, nil, registryAuthResponseLimit)
	if err != nil {
		return meResponse{}, err
	}
	if status == http.StatusUnauthorized {
		return meResponse{}, errUnauthorized
	}
	if status != http.StatusOK {
		return meResponse{}, fmt.Errorf("registry returned status %d", status)
	}
	var me meResponse
	if err := json.Unmarshal(data, &me); err != nil {
		return meResponse{}, err
	}
	if me.Login == "" {
		return meResponse{}, errors.New("registry returned no login")
	}
	if err := validatePrintableASCIIField("registry login", me.Login, maximumRegistryLoginLength); err != nil {
		return meResponse{}, err
	}
	return me, nil
}

// registryLogin obtains an API token and stores it for the current registry.
// Interactively it asks whether to open the device-authorization link in a
// browser or leave it for a headless/remote browser; --link and --code pick one
// without prompting. --token <t> uses a token directly and --with-token reads
// one from stdin (for CI).

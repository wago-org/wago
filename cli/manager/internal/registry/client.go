package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/internal/httpclient"
)

const registryResponseLimit int64 = 4 << 20

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
	if err := automation.RequireOnline("registry request"); err != nil {
		return 0, nil, err
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, registryBase()+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := registryHTTP.Bytes(ctx, req, registryResponseMaximum)
	return response.StatusCode, response.Body, err
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
	body, err := json.Marshal(map[string]string{"version": version})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	path := "/api/packages/" + url.PathEscape(module) + "/installs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registryBase()+path, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	_, _ = registryHTTP.Bytes(ctx, req, 4<<10)
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
	return fetchMeContext(context.Background(), token)
}

func fetchMeContext(ctx context.Context, token string) (meResponse, error) {
	status, data, err := apiRequestContext(ctx, http.MethodGet, "/api/me", token, nil)
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

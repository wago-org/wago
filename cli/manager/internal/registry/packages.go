package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

func resolveRegistryModule(name string) (string, error) {
	status, data, err := apiRequest(http.MethodGet, "/api/packages/"+url.PathEscape(name), "", nil)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", fmt.Errorf("no plugin %q in the registry", name)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("%s", apiError(status, data))
	}
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return "", err
	}
	if p.Name == "" {
		return "", fmt.Errorf("plugin %q has no module path", name)
	}
	return p.Name, nil
}

// registryPublish reads a wago.json manifest and POSTs it to /api/publish
// along with a version, commit, and optional metadata.

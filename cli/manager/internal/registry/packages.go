package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// closestModule returns the nearest package id in the registry when it is a
// plausible typo. Suggestions are best-effort and never block installation.
func closestModule(module string) string {
	status, data, err := apiRequest(http.MethodGet, "/api/packages", "", nil)
	if err != nil || status != http.StatusOK {
		return ""
	}
	var response struct {
		Packages []struct {
			ID string `json:"id"`
		} `json:"packages"`
	}
	if json.Unmarshal(data, &response) != nil {
		return ""
	}
	target := strings.TrimPrefix(module, "github.com/")
	best, distance := "", len(target)+1
	for _, candidate := range response.Packages {
		if d := editDistance(target, candidate.ID); d < distance {
			best, distance = candidate.ID, d
		}
	}
	limit := 2
	if len(target) > 12 {
		limit = 3
	}
	if distance == 0 || distance > limit {
		return ""
	}
	return best
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for i := range previous {
		previous[i] = i
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

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

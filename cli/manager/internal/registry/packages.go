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
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
)

const (
	maximumRegistrySuggestions = 1024
	maximumSuggestionIDLength  = 300
)

type packageSuggestion struct {
	ID string `json:"id"`
}

// closestModule returns the nearest package id in the registry when it is a
// plausible typo. Suggestions are best-effort and never block installation.
func closestModule(module string) string {
	return closestModuleContext(context.Background(), module)
}

func closestModuleContext(ctx context.Context, module string) string {
	status, data, err := apiRequestContext(ctx, http.MethodGet, "/api/packages", "", nil)
	if err != nil || status != http.StatusOK {
		return ""
	}
	var response struct {
		Packages []packageSuggestion `json:"packages"`
	}
	if preflightPackageSuggestions(data, maximumRegistrySuggestions) != nil ||
		unmarshalUniqueJSON(data, &response) != nil {
		return ""
	}
	target := strings.TrimPrefix(module, "github.com/")
	return closestModuleID(target, response.Packages)
}

func closestModuleID(target string, candidates []packageSuggestion) string {
	if !validSuggestionID(target) {
		return ""
	}
	limit := 2
	if len(target) > 12 {
		limit = 3
	}
	best, distance := "", limit+1
	var previous, current [maximumSuggestionIDLength + 1]int
	for _, candidate := range candidates {
		if !validSuggestionID(candidate.ID) {
			continue
		}
		if d, ok := editDistanceWithin(target, candidate.ID, limit, previous[:], current[:]); ok && d < distance {
			best, distance = candidate.ID, d
		}
	}
	if distance == 0 || distance > limit {
		return ""
	}
	return best
}

func validSuggestionID(id string) bool {
	if id == "" || len(id) > maximumSuggestionIDLength {
		return false
	}
	if project.ValidatePluginID(id) == nil {
		return true
	}
	return project.ValidatePluginID("registry.invalid/"+id) == nil
}

func editDistanceWithin(a, b string, limit int, previous, current []int) (int, bool) {
	if difference := len(a) - len(b); difference > limit || difference < -limit {
		return limit + 1, false
	}
	infinity := limit + 1
	for index := 0; index <= len(b); index++ {
		previous[index] = min(index, infinity)
	}
	for i := 1; i <= len(a); i++ {
		start, end := max(1, i-limit), min(len(b), i+limit)
		if start == 1 {
			current[0] = min(i, infinity)
		} else {
			current[start-1] = infinity
		}
		for j := start; j <= end; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		if end < len(b) {
			current[end+1] = infinity
		}
		previous, current = current, previous
	}
	distance := previous[len(b)]
	return distance, distance <= limit
}

func preflightPackageSuggestions(data []byte, maximum int) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("package response must be a JSON object")
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("package response contains a non-string key")
		}
		if !strings.EqualFold(key, "packages") {
			if err := skipPackageJSONValue(decoder); err != nil {
				return err
			}
			continue
		}
		array, err := decoder.Token()
		if err != nil || array != json.Delim('[') {
			return errors.New("package suggestions must be an array")
		}
		count := 0
		for decoder.More() {
			count++
			if count > maximum {
				return errors.New("package suggestions exceed the collection limit")
			}
			if err := skipPackageJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("package suggestions array is incomplete")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return errors.New("package response object is incomplete")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("package response contains trailing data")
	}
	return nil
}

func skipPackageJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return errors.New("package response contains an unexpected delimiter")
	}
	depth := 1
	for depth > 0 {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
		if delimiter, ok = token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

func resolveRegistryModule(name string) (string, error) {
	return resolveRegistryModuleContext(context.Background(), name)
}

func resolveRegistryModuleContext(ctx context.Context, name string) (string, error) {
	status, data, err := apiRequestContext(ctx, http.MethodGet, "/api/packages/"+url.PathEscape(name), "", nil)
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
	if err := unmarshalUniqueJSON(data, &p); err != nil {
		return "", errors.New("registry returned invalid package metadata")
	}
	if p.Name == "" {
		return "", fmt.Errorf("plugin %q has no module path", name)
	}
	if err := project.ValidatePluginID(p.Name); err != nil {
		return "", fmt.Errorf("plugin %q has an invalid module path", name)
	}
	return p.Name, nil
}

// registryPublish reads a wago.json manifest and POSTs it to /api/publish
// along with a version, commit, and optional metadata.

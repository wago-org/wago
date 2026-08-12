package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
)

func SplitCommaList(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

// AuthorityScopeOverrides is the strict JSON shape accepted by --scopes. The
// two-level key makes every override unambiguous when add or update resolves a
// graph containing multiple plugins that request the same Authority.
type AuthorityScopeOverrides map[string]map[string]project.AuthorityScope

func ParseAuthorityScopeOverrides(raw string) (AuthorityScopeOverrides, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("--scopes JSON exceeds 1 MiB")
	}
	if err := rejectDuplicateJSONKeys([]byte(raw)); err != nil {
		return nil, fmt.Errorf("--scopes: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var overrides AuthorityScopeOverrides
	if err := decoder.Decode(&overrides); err != nil {
		return nil, fmt.Errorf("--scopes: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("--scopes: multiple JSON values")
		}
		return nil, fmt.Errorf("--scopes: %w", err)
	}
	if len(overrides) == 0 {
		return nil, fmt.Errorf("--scopes must contain at least one plugin")
	}
	if len(overrides) > 128 {
		return nil, fmt.Errorf("--scopes contains %d plugins; maximum is 128", len(overrides))
	}
	for pluginID, authorities := range overrides {
		if err := project.ValidatePluginID(pluginID); err != nil {
			return nil, fmt.Errorf("--scopes plugin %q: %w", pluginID, err)
		}
		if len(authorities) == 0 {
			return nil, fmt.Errorf("--scopes plugin %q has no Authority overrides", pluginID)
		}
		if len(authorities) > 64 {
			return nil, fmt.Errorf("--scopes plugin %q has too many Authority overrides", pluginID)
		}
		for authority := range authorities {
			if authority == "" || authority != strings.TrimSpace(authority) || len(authority) > 200 {
				return nil, fmt.Errorf("--scopes plugin %q has invalid Authority %q", pluginID, authority)
			}
		}
	}
	return overrides, nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("object key is not a string")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil {
				return err
			}
			if closing != json.Delim('}') {
				return fmt.Errorf("invalid JSON object")
			}
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil {
				return err
			}
			if closing != json.Delim(']') {
				return fmt.Errorf("invalid JSON array")
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

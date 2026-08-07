package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InlineManifest resolves local subpackage manifest references before upload.
func InlineManifest(raw []byte, dir string) ([]byte, error) {
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	if err := inlineSubpackages(manifest, dir); err != nil {
		return nil, err
	}
	return json.Marshal(manifest)
}

func inlineSubpackages(manifest map[string]any, dir string) error {
	raw, ok := manifest["subpackages"].([]any)
	if !ok {
		return nil
	}
	for index, value := range raw {
		switch value := value.(type) {
		case string:
			path := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(value, "./")))
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("subpackage %q: %v", value, err)
			}
			var child map[string]any
			if err := json.Unmarshal(data, &child); err != nil {
				return fmt.Errorf("subpackage %q: %v", value, err)
			}
			if err := inlineSubpackages(child, filepath.Dir(path)); err != nil {
				return err
			}
			raw[index] = child
		case map[string]any:
			if err := inlineSubpackages(value, dir); err != nil {
				return err
			}
		default:
			return fmt.Errorf("subpackages[%d] must be a manifest object or path", index)
		}
	}
	manifest["subpackages"] = raw
	return nil
}

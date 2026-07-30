package wagocli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	projectFile       = "wago.json"
	manifestSchemaURI = "https://wago.sh/schema.json"
	manifestVersion   = "wago/v1"
)

func projectManifestPath(dir string) string { return filepath.Join(dir, projectFile) }

// readProjectMap loads wago.json as a generic map so initialization and plugin
// edits preserve fields owned by publishers and future schema versions.
func readProjectMap(dir string) (map[string]any, error) {
	b, err := os.ReadFile(projectManifestPath(dir))
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", projectFile, err)
	}
	return m, nil
}

func writeProjectMap(dir string, m map[string]any) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(projectManifestPath(dir), append(b, '\n'), 0o644)
}

func initializeProject(dir string) (bool, error) {
	_, statErr := os.Stat(projectManifestPath(dir))
	created := os.IsNotExist(statErr)
	m, err := readProjectMap(dir)
	if err != nil {
		return false, err
	}
	ensureProjectMetadata(m)
	if _, ok := m["dependencies"]; !ok {
		m["dependencies"] = []any{}
	}
	if _, ok := m["plugins"]; !ok {
		m["plugins"] = map[string]any{}
	}
	return created, writeProjectMap(dir, m)
}

func ensureProjectMetadata(m map[string]any) {
	if _, ok := m["$schema"]; !ok {
		m["$schema"] = manifestSchemaURI
	}
	if _, ok := m["schema"]; !ok {
		m["schema"] = manifestVersion
	}
}

// ensureGitignore appends entry to ./.gitignore if not already present. Best
// effort — a missing .gitignore is created only inside a git working tree.
func ensureGitignore(entry string) {
	const name = ".gitignore"
	b, err := os.ReadFile(name)
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(line)
			if t == entry || t == strings.TrimRight(entry, "/") {
				return
			}
		}
		f, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
			_, _ = f.WriteString("\n")
		}
		_, _ = f.WriteString(entry + "\n")
		return
	}
	if _, err := os.Stat(".git"); err == nil {
		_ = os.WriteFile(name, []byte(entry+"\n"), 0o644)
	}
}

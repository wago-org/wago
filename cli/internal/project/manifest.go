package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	File      = "wago.json"
	SchemaURI = "https://wago.sh/v0/schema.json"
)

func Path(dir string) string { return filepath.Join(dir, File) }

func DisplayPath(dir string) string {
	path, err := filepath.Abs(Path(dir))
	if err != nil {
		path = Path(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	path, home = filepath.Clean(path), filepath.Clean(home)
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// Read loads wago.json as a generic map so updates preserve fields owned by
// publishers and future schema versions.
func Read(dir string) (map[string]any, error) {
	data, err := os.ReadFile(Path(dir))
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	manifest := map[string]any{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", DisplayPath(dir), err)
	}
	return manifest, nil
}

func Write(dir string, manifest map[string]any) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(dir), append(data, '\n'), 0o644)
}

func Initialize(dir string) (bool, error) {
	_, statErr := os.Stat(Path(dir))
	created := os.IsNotExist(statErr)
	manifest, err := Read(dir)
	if err != nil {
		return false, err
	}
	EnsureMetadata(manifest)
	if _, ok := manifest["plugins"]; !ok {
		manifest["plugins"] = map[string]any{}
	}
	return created, Write(dir, manifest)
}

func EnsureMetadata(manifest map[string]any) {
	if _, ok := manifest["$schema"]; !ok {
		manifest["$schema"] = SchemaURI
	}
}

// EnsureGitignore appends entry to ./.gitignore if not already present. It is
// best effort and creates the file only inside a Git working tree.
func EnsureGitignore(entry string) {
	const name = ".gitignore"
	data, err := os.ReadFile(name)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			value := strings.TrimSpace(line)
			if value == entry || value == strings.TrimRight(entry, "/") {
				return
			}
		}
		file, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer file.Close()
		if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
			_, _ = file.WriteString("\n")
		}
		_, _ = file.WriteString(entry + "\n")
		return
	}
	if _, err := os.Stat(".git"); err == nil {
		_ = os.WriteFile(name, []byte(entry+"\n"), 0o644)
	}
}

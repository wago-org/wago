package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const LockFile = "wago-lock.json"

type LockEntry struct {
	Version              string          `json:"version,omitempty"`
	RequiredCapabilities []string        `json:"requiredCapabilities"`
	Capabilities         json.RawMessage `json:"capabilities"`
	Config               json.RawMessage `json:"config,omitempty"`
}

type LockDocument struct {
	Packages map[string]LockEntry `json:"plugins"`
}

func LockPath(dir string) string {
	return filepath.Join(dir, LockFile)
}

func ReadLock(dir string) (LockDocument, error) {
	document := LockDocument{Packages: map[string]LockEntry{}}
	data, err := os.ReadFile(LockPath(dir))
	if os.IsNotExist(err) {
		return document, nil
	}
	if err != nil {
		return LockDocument{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return LockDocument{}, fmt.Errorf("%s: %w", displayFilePath(LockPath(dir)), err)
	}
	if document.Packages == nil {
		document.Packages = map[string]LockEntry{}
	}
	return document, nil
}

func WriteLock(dir string, document LockDocument) error {
	if document.Packages == nil {
		document.Packages = map[string]LockEntry{}
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(LockPath(dir), append(data, '\n'), 0o644)
}

func Grants(dir, id string) []string {
	lock, err := ReadLock(dir)
	if err != nil {
		return nil
	}
	var grants []string
	_ = json.Unmarshal(lock.Packages[id].Capabilities, &grants)
	return grants
}

func SetGrants(dir, id string, capabilities []string) error {
	lock, err := ReadLock(dir)
	if err != nil {
		return err
	}
	sorted := append([]string(nil), capabilities...)
	sort.Strings(sorted)
	raw, err := json.Marshal(sorted)
	if err != nil {
		return err
	}
	entry := lock.Packages[id]
	entry.Capabilities = raw
	lock.Packages[id] = entry
	return WriteLock(dir, lock)
}

func SameStringSet(left, right []string) bool {
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func displayFilePath(path string) string {
	dir := filepath.Dir(path)
	if filepath.Base(path) == File {
		return DisplayPath(dir)
	}
	displayDir := DisplayPath(dir)
	if displayDir == "~" {
		return "~/" + filepath.Base(path)
	}
	return filepath.Join(displayDir, filepath.Base(path))
}

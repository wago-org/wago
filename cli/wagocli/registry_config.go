package wagocli

// Credentials store for the wago registry (pkg.wago.sh). This file is net-free
// (no net/http) so it compiles into both the full and the size-optimized
// (-tags wago_lean) builds; the actual HTTP calls live in registry_net.go.
//
// Credentials are keyed by registry base URL so several registries can coexist.
// They are stored at $XDG_CONFIG_HOME/wago/credentials.json (else
// $HOME/.config/wago/credentials.json).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// defaultRegistry is the public registry API base used when WAGO_REGISTRY is unset.
const defaultRegistry = "https://api.pkg.wago.sh"

// credential is one registry's stored token and the login it belongs to.
type credential struct {
	Token string `json:"token"`
	Login string `json:"login"`
}

// registryBase returns the registry API base URL: the WAGO_REGISTRY env var if
// set (trailing slash trimmed), else the public default.
func registryBase() string {
	if v := strings.TrimSpace(os.Getenv("WAGO_REGISTRY")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultRegistry
}

// frontendBase derives the website base URL (for package-page links) from the
// registry API base by dropping a leading "api." host label — e.g.
// https://api.pkg.wago.sh -> https://pkg.wago.sh. A base with no "api." host
// (like a localhost dev server) is returned unchanged.
func frontendBase() string {
	base := registryBase()
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(base, scheme) {
			rest := base[len(scheme):]
			if strings.HasPrefix(rest, "api.") {
				return scheme + rest[len("api."):]
			}
			return base
		}
	}
	return base
}

// shortFromModule derives a package short id from a module path: the last path
// element with a leading "wago-" or "wago_" stripped. This mirrors the registry
// server so CLI-printed package-page URLs match.
func shortFromModule(module string) string {
	short := module
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	short = strings.TrimPrefix(short, "wago-")
	short = strings.TrimPrefix(short, "wago_")
	return short
}

// ownerFromModule extracts the GitHub owner from a module path, e.g.
// "github.com/wago-org/wasi" -> "wago-org". Empty when not a github path.
func ownerFromModule(module string) string {
	const host = "github.com/"
	i := strings.Index(module, host)
	if i < 0 {
		return ""
	}
	rest := module[i+len(host):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return ""
}

// packageURL builds the canonical registry page URL for a module:
// {frontend}/{owner}/{short}.
func packageURL(module string) string {
	owner := ownerFromModule(module)
	if owner == "" {
		owner = "packages"
	}
	return frontendBase() + "/" + owner + "/" + shortFromModule(module)
}

// credentialsPath returns the path to the credentials file.
func credentialsPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "wago", "credentials.json")
}

// loadCredentials reads the registry->credential map. A missing file yields an
// empty map (not an error); a malformed file yields an error.
func loadCredentials() (map[string]credential, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]credential{}, nil
		}
		return nil, err
	}
	creds := map[string]credential{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return creds, nil
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return creds, nil
}

// saveCredentials records token+login for base, preserving other registries'
// entries, and writes the file with 0600 permissions.
func saveCredentials(base, token, login string) error {
	creds, err := loadCredentials()
	if err != nil {
		return err
	}
	creds[base] = credential{Token: token, Login: login}
	return writeCredentials(creds)
}

// deleteCredentials removes the stored entry for base (a no-op if none exists).
func deleteCredentials(base string) error {
	creds, err := loadCredentials()
	if err != nil {
		return err
	}
	if _, ok := creds[base]; !ok {
		return nil
	}
	delete(creds, base)
	return writeCredentials(creds)
}

// writeCredentials serializes the credential map to disk (0600), creating the
// parent directory if needed.
func writeCredentials(creds map[string]credential) error {
	path := credentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// resolveToken returns the API token for the current registry: WAGO_TOKEN wins,
// else the stored token for the current base. Returns "" when there is none.
func resolveToken() string {
	if v := strings.TrimSpace(os.Getenv("WAGO_TOKEN")); v != "" {
		return v
	}
	creds, err := loadCredentials()
	if err != nil {
		return ""
	}
	return creds[registryBase()].Token
}

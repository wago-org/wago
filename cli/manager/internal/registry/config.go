package registry

// Credentials store for the wago registry (plugins.wago.sh). This file is net-free
// (no net/http) and is shared by the manager and runtime builds; the actual HTTP
// calls live in the registry client and workflow files.
//
// Credentials are keyed by registry base URL so several registries can coexist.
// They use Wago's platform config directory: ~/.wago/config on macOS and the
// XDG config directory on Linux, unless WAGO_HOME overrides both.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/filelock"
	"github.com/wago-org/wago/internal/wagopaths"
)

const maximumCredentialStoreSize int64 = 1 << 20

// defaultRegistry is the public registry API base used when WAGO_REGISTRY is unset.
const defaultRegistry = "https://api.plugins.wago.sh"

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

// BaseURL returns the configured registry API origin for narrow consumers such
// as the v1 plugin catalog adapter.
func BaseURL() string { return registryBase() }

// frontendBase derives the website base URL (for package-page links) from the
// registry API base by dropping a leading "api." host label — e.g.
// https://api.plugins.wago.sh -> https://plugins.wago.sh. A base with no "api." host
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
	return filepath.Join(wagopaths.DirsFor("manager").Config, "credentials.json")
}

// loadCredentials reads the registry->credential map. A missing file yields an
// empty map (not an error); a malformed file yields an error.
func loadCredentials() (_ map[string]credential, resultErr error) {
	path := credentialsPath()
	file, err := openCredentialFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]credential{}, nil
		}
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if info, err := file.Stat(); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, errors.New("credential store must be a regular file")
	} else if info.Size() > maximumCredentialStoreSize {
		return nil, fmt.Errorf("credential store exceeds %d-byte limit", maximumCredentialStoreSize)
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumCredentialStoreSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximumCredentialStoreSize {
		return nil, fmt.Errorf("credential store exceeds %d-byte limit", maximumCredentialStoreSize)
	}
	creds := map[string]credential{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return creds, nil
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	if creds == nil {
		return nil, errors.New("credential store must be a JSON object")
	}
	return creds, nil
}

// saveCredentials records token+login for base while holding the cross-process
// credential-store lock, preserving other registries' latest entries.
func saveCredentials(base, token, login string) error {
	return saveCredentialsContext(context.Background(), base, token, login)
}

func saveCredentialsContext(ctx context.Context, base, token, login string) error {
	if err := validateRegistryToken(token); err != nil {
		return err
	}
	if login == "" {
		return errors.New("registry login is empty")
	}
	if err := validatePrintableASCIIField("registry login", login, maximumRegistryLoginLength); err != nil {
		return err
	}
	_, err := mutateCredentials(ctx, func(creds map[string]credential) bool {
		creds[base] = credential{Token: token, Login: login}
		return true
	})
	return err
}

// deleteCredentials removes the stored entry for base (a no-op if none exists).
func deleteCredentials(base string) error {
	return deleteCredentialsContext(context.Background(), base)
}

func deleteCredentialsContext(ctx context.Context, base string) error {
	_, err := deleteCredentialsResultContext(ctx, base)
	return err
}

func deleteCredentialsResultContext(ctx context.Context, base string) (bool, error) {
	return mutateCredentials(ctx, func(creds map[string]credential) bool {
		if _, ok := creds[base]; !ok {
			return false
		}
		delete(creds, base)
		return true
	})
}

func mutateCredentials(ctx context.Context, mutate func(map[string]credential) bool) (bool, error) {
	if ctx == nil {
		return false, errors.New("nil credential mutation context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	path := credentialsPath()
	if err := prepareCredentialDirectory(filepath.Dir(path)); err != nil {
		return false, err
	}
	lock, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return false, err
	}
	creds, operationErr := loadCredentials()
	changed := false
	if operationErr == nil {
		changed = mutate(creds)
		if changed {
			operationErr = writeCredentialsLocked(path, creds)
		}
	}
	return changed, errors.Join(operationErr, lock.Close())
}

// writeCredentials replaces the complete credential map under the same lock
// used by mutations. Existing malformed data is rejected rather than silently
// overwritten.
func writeCredentials(creds map[string]credential) error {
	path := credentialsPath()
	if err := prepareCredentialDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	lock, err := filelock.Acquire(context.Background(), path+".lock")
	if err != nil {
		return err
	}
	_, operationErr := loadCredentials()
	if operationErr == nil {
		operationErr = writeCredentialsLocked(path, creds)
	}
	return errors.Join(operationErr, lock.Close())
}

var (
	replaceCredentialFile = atomicfile.ReplaceFile
	credentialAtomicHooks *atomicfile.Hooks
)

func writeCredentialsLocked(path string, creds map[string]credential) error {
	if creds == nil {
		return errors.New("credential store must be a JSON object")
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if int64(len(data)) > maximumCredentialStoreSize {
		return fmt.Errorf("credential store exceeds %d-byte limit", maximumCredentialStoreSize)
	}
	return replaceCredentialFile(path, atomicfile.Options{Mode: 0o600, Sync: true, Hooks: credentialAtomicHooks}, func(writer io.Writer) error {
		_, err := writer.Write(data)
		return err
	})
}

func prepareCredentialDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	// Windows mode bits do not provide ACL-equivalent privacy guarantees. The
	// file remains atomically replaced there, while native ACL hardening is a
	// separate platform concern.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(directory, 0o700); err != nil {
			return err
		}
	}
	return nil
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

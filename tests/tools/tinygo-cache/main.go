// Command tinygo-cache verifies and stamps the runner-toolcache installation
// used by the pinned TinyGo setup action.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const actionRevision = "db56321a62b9a67922bb9ac8f9d085e218807bb3"

func main() {
	if len(os.Args) != 2 {
		fail(errors.New("usage: tinygo-cache verify | record"))
	}
	root, err := installRoot(os.Getenv("RUNNER_TOOL_CACHE"), os.Getenv("TINYGO_VERSION"), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fail(err)
	}
	goVersion := os.Getenv("TINYGO_GO_VERSION")
	if goVersion == "" {
		fail(errors.New("TINYGO_GO_VERSION is required"))
	}
	if os.Args[1] == "verify" {
		if err := verify(root, os.Getenv("TINYGO_VERSION"), goVersion, os.Getenv("TINYGO_CACHE_HIT") == "true"); err != nil {
			fail(err)
		}
		return
	}
	if os.Args[1] != "record" {
		fail(errors.New("usage: tinygo-cache verify | record"))
	}
	if err := record(root, os.Getenv("TINYGO_VERSION"), goVersion); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tinygo-cache:", err)
	os.Exit(1)
}

func installRoot(toolCache, version, goos, goarch string) (string, error) {
	if toolCache == "" || version == "" {
		return "", errors.New("RUNNER_TOOL_CACHE and TINYGO_VERSION are required")
	}
	var actionArch string
	switch {
	case goos == "linux" && goarch == "arm64":
		actionArch = "arm64"
	case goos == "darwin" && goarch == "amd64":
		actionArch = "x86_64"
	case (goos == "linux" || goos == "windows") && goarch == "amd64":
		actionArch = "amd64"
	case goos == "darwin" && goarch == "arm64":
		actionArch = "arm64"
	default:
		return "", fmt.Errorf("unsupported TinyGo runner %s/%s", goos, goarch)
	}
	return filepath.Join(toolCache, "tinygo", version, actionArch), nil
}

func verify(root, tinygoVersion, goVersion string, cacheHit bool) error {
	stamp := filepath.Join(root, ".wago-provenance")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(stamp); errors.Is(err, os.ErrNotExist) && !cacheHit {
		// A runner image may already contain the pinned tool. Record it after the
		// setup action selects it; only Actions cache entries need our stamp.
		return nil
	}
	actual, err := provenance(root, tinygoVersion, goVersion)
	if err != nil {
		if !cacheHit && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return discardInvalid(root, err)
	}
	data, err := os.ReadFile(stamp)
	if err == nil && string(data) == actual {
		return nil
	}
	if !cacheHit && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return discardInvalid(root, errors.New("cached TinyGo provenance or tree digest does not match"))
}

func discardInvalid(root string, cause error) error {
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("invalid TinyGo cache (%v); remove %s: %w", cause, root, err)
	}
	fmt.Fprintf(os.Stderr, "tinygo-cache: discarded unverified toolcache entry: %v\n", cause)
	return nil
}

func record(root, tinygoVersion, goVersion string) error {
	data, err := provenance(root, tinygoVersion, goVersion)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	stamp := filepath.Join(root, ".wago-provenance")
	tmp := stamp + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, stamp)
}

func provenance(root, tinygoVersion, goVersion string) (string, error) {
	bin := filepath.Join(root, "tinygo", "bin", "tinygo")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if info, err := os.Stat(bin); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("TinyGo executable is missing from %s", bin)
	}
	output, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil || !strings.Contains(string(output), "version "+tinygoVersion) {
		return "", fmt.Errorf("TinyGo version check failed for %s: %s", bin, strings.TrimSpace(string(output)))
	}
	digest, err := treeDigest(filepath.Join(root, "tinygo"))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tinygo=%s\ngo=%s\naction=%s\ntree_sha256=%s\n", tinygoVersion, goVersion, actionRevision, digest), nil
}

func treeDigest(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%o\x00", filepath.ToSlash(rel), info.Mode())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			io.WriteString(hash, target)
			hash.Write([]byte{0})
			return nil
		}
		if entry.IsDir() {
			hash.Write([]byte{0})
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(hash, file); err != nil {
			file.Close()
			return err
		}
		hash.Write([]byte{0})
		return file.Close()
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

//go:build wago_lean

// Lean/TinyGo build: use the host curl executable for the small downloader
// surface. TinyGo has no ordinary host socket transport, while curl provides
// HTTPS, certificate verification, and redirect handling without retaining a
// Go HTTP stack in the release binary.

package wagocli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago"
)

func vmInstall(d wago.Dirs, ver string) {
	dest := d.VersionBinary(ver)
	if fi, err := os.Stat(dest); err == nil && !fi.IsDir() {
		fmt.Printf("wago %s already installed\n", ver)
		return
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version install: %v", err)
	}
	if err := downloadBinary(releaseBase(), ver, dest); err != nil {
		fatal("version install: %v", err)
	}
	fmt.Printf("installed wago %s -> %s\n", cyan(ver), dest)
}

func vmUpdate(d wago.Dirs, ver string) {
	dest := d.VersionBinary(ver)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version update: %v", err)
	}
	if err := downloadBinary(releaseBase(), ver, dest); err != nil {
		fatal("version update: %v", err)
	}
	fmt.Printf("updated wago %s -> %s\n", cyan(ver), dest)
}

func vmListRemote() {
	body, err := curlGetBytes(releaseAPI() + "/repos/wago-org/wago/releases")
	if err != nil {
		fatal("version list-remote: %v", err)
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		fatal("version list-remote: %v", err)
	}
	if len(releases) == 0 {
		fmt.Println(dim("no releases published"))
		return
	}
	for _, r := range releases {
		fmt.Println(strings.TrimPrefix(r.TagName, "v"))
	}
}

// downloadBinary verifies the sibling SHA-256 before atomically replacing dest.
func downloadBinary(baseURL, ver, dest string) error {
	asset := "wago-" + runtime.GOOS + "-" + runtime.GOARCH
	url := fmt.Sprintf("%s/%s/%s", strings.TrimRight(baseURL, "/"), ver, asset)
	body, err := curlGetBytes(url)
	if err != nil {
		return err
	}
	sumRaw, err := curlGetBytes(url + ".sha256")
	if err != nil {
		return fmt.Errorf("fetch checksum: %w", err)
	}
	want := strings.TrimSpace(string(sumRaw))
	if fields := strings.Fields(want); len(fields) > 0 {
		want = fields[0]
	}
	got := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return fmt.Errorf("checksum mismatch for %s (want %s, got %x)", asset, want, got)
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, body, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// curlGetBytes runs curl without a shell: URL text is always one argument, so a
// requested version cannot become an option or command. --location follows the
// GitHub release-asset redirect and --fail turns non-2xx responses into errors.
func curlGetBytes(url string) ([]byte, error) {
	cmd := exec.Command("curl",
		"--fail", "--location", "--silent", "--show-error",
		"--connect-timeout", "10", "--max-time", "120", "--", url)
	body, err := cmd.Output()
	if err == nil {
		return body, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
		return nil, fmt.Errorf("curl: %s", strings.TrimSpace(string(exitErr.Stderr)))
	}
	return nil, fmt.Errorf("curl: %w", err)
}

func releaseBase() string {
	if v := os.Getenv("WAGO_RELEASE_BASE"); v != "" {
		return v
	}
	return "https://github.com/wago-org/wago/releases/download"
}

func releaseAPI() string {
	if v := os.Getenv("WAGO_RELEASE_API"); v != "" {
		return v
	}
	return "https://api.github.com"
}

//go:build !wago_lean

// The full version manager pulls in net/http, encoding/json, and crypto/sha256.
// It is excluded from the size-optimized/TinyGo build (-tags wago_lean), which
// gets the stub in version_stub.go instead, keeping the release binary lean.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

// versionCmd handles `wago version` (print version, default) and the version
// manager subcommands (`wago version list|install|use|...`).
func versionCmd(args []string) {
	if len(args) == 0 {
		printVersion()
		return
	}
	d := wago.DirsFor(versionString())
	switch args[0] {
	case "list", "ls":
		vmList(d)
	case "current":
		vmCurrent(d)
	case "which":
		vmWhich(d)
	case "use":
		vmUse(d, versionArg("use", args[1:]))
	case "install", "add":
		vmInstall(d, versionArg("install", args[1:]))
	case "uninstall", "remove", "rm":
		vmUninstall(d, versionArg("uninstall", args[1:]))
	case "list-remote", "ls-remote":
		vmListRemote()
	default:
		fatal("version: unknown subcommand %q (have: list, current, which, use, install, uninstall, list-remote)", args[0])
	}
}

func versionArg(sub string, args []string) string {
	if len(args) != 1 || args[0] == "" {
		fatal("version %s: need a <version>", sub)
	}
	return args[0]
}

// ---- installed-version state --------------------------------------------

// installedVersions returns the versions that have an installed binary, sorted.
func installedVersions(d wago.Dirs) []string {
	entries, err := os.ReadDir(d.Versions)
	if err != nil {
		return nil
	}
	var vers []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if fi, err := os.Stat(d.VersionBinary(e.Name())); err == nil && !fi.IsDir() {
			vers = append(vers, e.Name())
		}
	}
	sort.Slice(vers, func(i, j int) bool { return compareSemver(vers[i], vers[j]) < 0 })
	return vers
}

func activeVersion(d wago.Dirs) string {
	b, err := os.ReadFile(d.ConfigFile("active-version"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func setActiveVersion(d wago.Dirs, ver string) error {
	if err := d.Ensure(); err != nil {
		return err
	}
	if err := os.WriteFile(d.ConfigFile("active-version"), []byte(ver+"\n"), 0o644); err != nil {
		return err
	}
	// Best-effort convenience symlink: <data>/bin/wago -> the active binary. Add
	// <data>/bin to PATH to make `wago` resolve to the selected version.
	binDir := filepath.Join(d.Data, "bin")
	if err := os.MkdirAll(binDir, 0o755); err == nil {
		link := filepath.Join(binDir, "wago")
		_ = os.Remove(link)
		_ = os.Symlink(d.VersionBinary(ver), link)
	}
	return nil
}

// ---- subcommands --------------------------------------------------------

func vmList(d wago.Dirs) {
	vers := installedVersions(d)
	if len(vers) == 0 {
		fmt.Println(dim("no versions installed; try: wago version install <ver>"))
		return
	}
	active := activeVersion(d)
	for _, v := range vers {
		marker := "  "
		if v == active {
			marker = cyan("* ")
		}
		fmt.Printf("%s%s\n", marker, v)
	}
}

func vmCurrent(d wago.Dirs) {
	if a := activeVersion(d); a != "" {
		fmt.Println(a)
		return
	}
	fmt.Println(dim("no active version set"))
}

func vmWhich(d wago.Dirs) {
	a := activeVersion(d)
	if a == "" {
		fatal("version which: no active version set")
	}
	fmt.Println(d.VersionBinary(a))
}

func vmUse(d wago.Dirs, ver string) {
	if fi, err := os.Stat(d.VersionBinary(ver)); err != nil || fi.IsDir() {
		fatal("version use: %s is not installed (try: wago version install %s)", ver, ver)
	}
	if err := setActiveVersion(d, ver); err != nil {
		fatal("version use: %v", err)
	}
	fmt.Printf("now using wago %s\n", cyan(ver))
}

func vmUninstall(d wago.Dirs, ver string) {
	dir := filepath.Join(d.Versions, ver)
	if _, err := os.Stat(dir); err != nil {
		fatal("version uninstall: %s is not installed", ver)
	}
	if err := os.RemoveAll(dir); err != nil {
		fatal("version uninstall: %v", err)
	}
	if activeVersion(d) == ver {
		_ = os.Remove(d.ConfigFile("active-version"))
	}
	fmt.Printf("uninstalled wago %s\n", ver)
}

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

func vmListRemote() {
	base := releaseAPI()
	resp, err := http.Get(base + "/repos/wago-org/wago/releases")
	if err != nil {
		fatal("version list-remote: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal("version list-remote: GitHub returned %s", resp.Status)
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
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

// ---- download -----------------------------------------------------------

// downloadBinary fetches the linux/amd64 wago binary for ver from baseURL,
// verifies its SHA-256 against the sibling ".sha256" file, and writes it to dest
// (0755). It writes nothing on a checksum mismatch.
func downloadBinary(baseURL, ver, dest string) error {
	asset := "wago-linux-amd64"
	url := fmt.Sprintf("%s/%s/%s", strings.TrimRight(baseURL, "/"), ver, asset)

	body, err := httpGetBytes(url)
	if err != nil {
		return err
	}
	sumRaw, err := httpGetBytes(url + ".sha256")
	if err != nil {
		return fmt.Errorf("fetch checksum: %w", err)
	}
	want := strings.TrimSpace(string(sumRaw))
	if fields := strings.Fields(want); len(fields) > 0 {
		want = fields[0] // accept "<hash>  <filename>" form
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

func httpGetBytes(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// releaseBase is the base URL for release binary assets, overridable for testing.
func releaseBase() string {
	if v := os.Getenv("WAGO_RELEASE_BASE"); v != "" {
		return v
	}
	return "https://github.com/wago-org/wago/releases/download"
}

// releaseAPI is the GitHub API base, overridable for testing.
func releaseAPI() string {
	if v := os.Getenv("WAGO_RELEASE_API"); v != "" {
		return v
	}
	return "https://api.github.com"
}

// compareSemver does a numeric dotted compare of two version strings, ignoring a
// leading 'v'. Non-numeric components sort after numeric ones.
func compareSemver(a, b string) int {
	as := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bs := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		var ao, bo bool
		if i < len(as) {
			av, ao = atoiOK(as[i])
		}
		if i < len(bs) {
			bv, bo = atoiOK(bs[i])
		}
		if ao && bo {
			if av != bv {
				return sign(av - bv)
			}
			continue
		}
		if c := strings.Compare(get(as, i), get(bs, i)); c != 0 {
			return c
		}
	}
	return 0
}

func atoiOK(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func get(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

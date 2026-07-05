package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

// printVersion prints the binary version and supported features (wago --version).
func printVersion() {
	fmt.Printf("%s %s (linux/amd64)\n", bold("wago"), versionString())
	fmt.Printf("%s %s\n", dim("features:"), wago.SupportedFeatures())
	if wago.GuardPageSupported() {
		fmt.Printf("%s signals-based bounds checks available\n", dim("guard-page:"))
	}
}

// versionCmd is the version manager (`wago version list|use|install|...`). The
// binary's own version is printed by `wago --version` / `wago -v`. Version
// management (list/use/current/which/uninstall) is net-free and ships in every
// build; only the downloader (install/list-remote) requires the full build, since
// TinyGo cannot link net/http (see version_net.go vs version_net_stub.go).
func versionCmd(args []string) {
	d := wago.DirsFor(versionString())
	if len(args) == 0 {
		vmList(d)
		return
	}
	switch args[0] {
	case "list", "ls":
		vmList(d)
	case "current":
		vmCurrent(d)
	case "which":
		vmWhich(d)
	case "use":
		vmUse(d, versionArg("use", args[1:]))
	case "uninstall", "remove", "rm":
		vmUninstall(d, versionArg("uninstall", args[1:]))
	case "install", "add":
		vmInstall(d, versionArg("install", args[1:]))
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

// ---- installed-version state (net-free) ---------------------------------

// installedVersions returns the versions that have an installed binary, sorted
// in numeric semver order.
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

// ---- net-free subcommands -----------------------------------------------

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

// ---- semver ordering ----------------------------------------------------

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
	if s == "" {
		return 0, false
	}
	n := 0
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

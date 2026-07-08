// Custom-build machinery: turn a wago-plugins.json manifest into a wago binary
// with exactly those plugins compiled in, cached so a rebuild only happens when
// the plugin set (or the toolchain) changes.
//
// Plugins stay native Go for full speed and to support any plugin kind (host
// imports, codegen backends, …). The build is generic: for each plugin we
// blank-import its `register` package, whose init() self-registers it with the
// engine. There are no build tags and no per-plugin code in wago — WASI is just
// another entry. The generated import file is injected into cli/wago via `go
// build -overlay`, so the wago source tree is never modified.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// maybeReexecForPlugins transparently hands off to the custom wago binary that
// has this project's manifest plugins compiled in — building it once (then cache
// hits), so `wago run` "just works" with the declared plugins. It's a no-op when
// there's no manifest, when the manifest has no module plugins, or when we're
// already running a plugin-built binary (guarded by WAGO_PLUGIN_ACTIVE). A build
// failure (e.g. the wago source can't be located) degrades to a warning so the
// current binary still runs.
func maybeReexecForPlugins() {
	if os.Getenv("WAGO_PLUGIN_ACTIVE") != "" {
		return
	}
	m, err := loadManifest(manifestPath())
	if err != nil {
		return
	}
	var enabled []PluginEntry
	for _, e := range m.Plugins {
		if e.Enabled && !e.Builtin() {
			enabled = append(enabled, e)
		}
	}
	if len(enabled) == 0 {
		return
	}
	bin, _, err := ensurePluginBinary(enabled)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s could not build plugins (%v); running without them\n", dim("wago:"), err)
		return
	}
	env := append(os.Environ(), "WAGO_PLUGIN_ACTIVE="+pluginBuildHash(enabled))
	if err := execProcess(bin, append([]string{bin}, os.Args[1:]...), env); err != nil {
		fatal("plugins: exec %s: %v", bin, err)
	}
}

// pluginBuild builds (or reuses a cached) custom wago binary for the manifest.
func pluginBuild() {
	enabled := enabledModulePlugins()
	fmt.Printf("%s\n", bold("building a custom wago binary with:"))
	for _, e := range enabled {
		fmt.Printf("  %s  %s\n", cyan(e.Name), dim(e.source()))
	}
	bin, cached, err := ensurePluginBinary(enabled)
	if err != nil {
		fatal("plugin build: %v", err)
	}
	verb := "built"
	if cached {
		verb = "up to date"
	}
	fmt.Printf("%s %s  %s\n", cyan("✓"), verb, bin)
}

// enabledModulePlugins returns the manifest's enabled, module-backed plugins (a
// "builtin" entry needs no build), fatal-ing if there's nothing to build.
func enabledModulePlugins() []PluginEntry {
	path := manifestPath()
	m, err := loadManifest(path)
	if err != nil {
		fatal("plugin build: %v", err)
	}
	var enabled []PluginEntry
	for _, e := range m.Plugins {
		if e.Enabled && !e.Builtin() {
			enabled = append(enabled, e)
		}
	}
	if len(enabled) == 0 {
		fatal("plugin build: no enabled plugins in %s (add one: wago pkg add <module>)", path)
	}
	return enabled
}

// source is a human-readable "module@version" (or bare module) for display.
func (e PluginEntry) source() string {
	if e.Version != "" {
		return e.Module + "@" + e.Version
	}
	return e.Module
}

// registerImport is the package a build blank-imports to pull in and self-register
// the plugin: the module's conventional `register` subpackage.
func (e PluginEntry) registerImport() string { return e.Module + "/register" }

// ensurePluginBinary returns the path to a cached wago binary with the given
// plugins compiled in, building it if absent. cached reports a cache hit.
func ensurePluginBinary(plugins []PluginEntry) (bin string, cached bool, err error) {
	wagoDir, err := wagoModuleDir()
	if err != nil {
		return "", false, err
	}
	dir := filepath.Join(pluginCacheDir(), pluginBuildHash(plugins))
	bin = filepath.Join(dir, "wago"+exeSuffix())
	if _, err := os.Stat(bin); err == nil {
		return bin, true, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}

	tmp, err := os.MkdirTemp("", "wago-plugin-build-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(tmp)

	gen := filepath.Join(tmp, "zz_wago_plugins.go")
	if err := os.WriteFile(gen, []byte(generatePluginsFile(plugins)), 0o644); err != nil {
		return "", false, err
	}
	// Inject the generated file into cli/wago at build time (no source edits).
	overlay := map[string]map[string]string{"Replace": {
		filepath.Join(wagoDir, "cli", "wago", "zz_wago_plugins.go"): gen,
	}}
	ov := filepath.Join(tmp, "overlay.json")
	ob, _ := json.Marshal(overlay)
	if err := os.WriteFile(ov, ob, 0o644); err != nil {
		return "", false, err
	}

	cmd := exec.Command("go", "build", "-overlay", ov, "-o", bin, "./cli/wago")
	cmd.Dir = wagoDir
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir) // don't leave a half-built cache entry
		return "", false, fmt.Errorf("go build: %w", err)
	}
	return bin, false, nil
}

// generatePluginsFile is the package-main source that blank-imports each plugin's
// register package.
func generatePluginsFile(plugins []PluginEntry) string {
	var b strings.Builder
	b.WriteString("// Code generated by `wago plugin build`. DO NOT EDIT.\npackage main\n\n")
	if len(plugins) == 0 {
		return b.String()
	}
	b.WriteString("import (\n")
	for _, e := range plugins {
		fmt.Fprintf(&b, "\t_ %q\n", e.registerImport())
	}
	b.WriteString(")\n")
	return b.String()
}

// pluginBuildHash keys the cache on the exact plugin set plus the toolchain, so a
// change in either forces a rebuild.
func pluginBuildHash(plugins []PluginEntry) string {
	keys := make([]string, len(plugins))
	for i, e := range plugins {
		keys[i] = e.Module + "@" + e.Version
	}
	sort.Strings(keys)
	h := sha256.New()
	fmt.Fprintf(h, "wago-plugin-build\x00%s\x00%s\x00%s/%s\x00", versionString(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
	for _, k := range keys {
		fmt.Fprintf(h, "%s\x00", k)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// pluginCacheDir is where built custom binaries live, one per hash.
func pluginCacheDir() string {
	if d := os.Getenv("WAGO_PLUGIN_CACHE"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "wago-plugins")
	}
	return filepath.Join(home, ".wago", "plugins")
}

// wagoModuleDir locates the wago source to build from. Uses WAGO_SRC if set, else
// the current Go module when that is github.com/wago-org/wago.
func wagoModuleDir() (string, error) {
	if d := os.Getenv("WAGO_SRC"); d != "" {
		return d, nil
	}
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("locating wago source: %w (set WAGO_SRC to the wago checkout)", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		return "", fmt.Errorf("not inside a Go module; set WAGO_SRC to the wago checkout")
	}
	if b, err := os.ReadFile(gomod); err != nil || !strings.Contains(string(b), "module github.com/wago-org/wago") {
		return "", fmt.Errorf("current module is not github.com/wago-org/wago; set WAGO_SRC to the wago checkout")
	}
	return filepath.Dir(gomod), nil
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

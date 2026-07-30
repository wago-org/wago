//go:build !wago_manager

// The consume side of `wago add` / `wago plugin`: declaring version-constrained
// plugins in wago.json, resolving them with Go modules, and building/running a
// custom Wago that has them compiled in. The build machinery lives in
// wagomodule.go.
package wagocli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// pkgOpts are the shared flags for the consume-side pkg commands.
type pkgOpts struct {
	global  bool // operate on the ~/.wago set instead of the project
	force   bool // ignore the build cache / fetch latest
	verbose bool // stream the underlying `go` output
}

type packageInstall struct {
	module    string
	requested string
	resolved  string
	exact     string
}

// handoffPluginProcess is the process-replacement boundary used after resolving
// a plugin-aware Wago build. Tests replace it so selection can be verified
// without replacing the test process.
var handoffPluginProcess = execProcess

// plural returns "s" unless n == 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pkgAddMany(specs []string, o pkgOpts) {
	started := time.Now()
	progress := newInstallProgress(os.Stderr)
	if o.verbose {
		progress.tty = false
	}
	progress.title("Installing packages")
	progress.begin("Resolving packages")
	packages, err := resolvePackageInstalls(specs)
	if err != nil {
		progress.fail("Package resolution failed")
		fatal("add: %v", err)
	}

	buildDir, err := buildDirFor(o.global)
	if err != nil {
		progress.fail("Package resolution failed")
		fatal("add: %v", err)
	}
	if err := ensureBuildModule(buildDir); err != nil {
		progress.fail("Package resolution failed")
		fatal("add: %v", err)
	}
	progress.done(fmt.Sprintf("Resolved %d package%s", len(packages), plural(len(packages))))

	progress.begin("Downloading packages")
	for _, pkg := range packages {
		getSpec := pkg.module
		if pkg.requested != "" {
			getSpec += "@" + pkg.requested
		}
		var getErr error
		if o.force && pkg.requested == "" {
			getErr = goUpdate(buildDir, pkg.module, o.verbose)
		} else {
			getErr = goGetDep(buildDir, getSpec, o.verbose)
		}
		if getErr != nil {
			progress.fail("Package download failed")
			if _, haveSrc := wagoSourceDir(); !haveSrc {
				fatal("add: fetching %s: %v\n  (during dev, set WAGO_SRC to a wago checkout so sibling plugins resolve locally)", getSpec, getErr)
			}
			fatal("add: fetching %s: %v", getSpec, getErr)
		}
	}
	progress.done(fmt.Sprintf("Downloaded %d package%s", len(packages), plural(len(packages))))

	progress.begin("Verifying packages")
	if err := goRun(buildDir, o.verbose, "mod", "verify"); err != nil {
		progress.fail("Package verification failed")
		fatal("add: verifying packages: %v", err)
	}
	progress.done("Verified package checksums")

	src, err := depsSource(o.global)
	if err != nil {
		progress.fail("Package setup failed")
		fatal("add: %v", err)
	}
	if o.global {
		if err := os.MkdirAll(src, 0o755); err != nil {
			progress.fail("Package setup failed")
			fatal("add: %v", err)
		}
	}
	for i := range packages {
		packages[i].exact = installedModuleExactVersion(buildDir, packages[i].module, packages[i].requested)
		packages[i].resolved = displayModuleVersion(packages[i].exact)
		if _, err := addProjectDep(src, packages[i].module, "^"+packages[i].resolved); err != nil {
			progress.fail("Package setup failed")
			fatal("add: %v", err)
		}
	}
	if !o.global {
		ensureGitignore(".wago/")
	}
	deps, _ := projectDeps(src)
	_ = writeBuildMain(buildDir, deps) // keep the build module in sync

	// Rebuild the custom binary right away so the package is usable without a
	// separate build step.
	progress.begin("Building packages")
	bin, cached, err := buildPlugins(buildDir, deps, o)
	if err != nil {
		progress.fail("Package build failed")
		fatal("build: %v", err)
	}
	elapsed := time.Since(started)
	if cached {
		progress.finish(fmt.Sprintf("Reused Wago build with %d package%s", len(packages), plural(len(packages))))
	} else {
		progress.finish(fmt.Sprintf("Built Wago with %d package%s", len(packages), plural(len(packages))))
	}
	for _, pkg := range packages {
		recordRegistryInstall(pkg.module, pkg.resolved)
	}
	// Then review capabilities — on a first install, or when the package's
	// required capabilities have changed since the lockfile recorded them.
	for _, pkg := range packages {
		reviewInstalledCapabilities(src, bin, pkg.module, pkg.exact)
	}
	printPackageInstallSummary(os.Stdout, packages, elapsed)
}

func resolvePackageInstalls(specs []string) ([]packageInstall, error) {
	packages := make([]packageInstall, 0, len(specs))
	seen := make(map[string]string, len(specs))
	for _, spec := range specs {
		module, version := splitModuleVersion(normalizeModuleRef(spec))
		if module == "" {
			return nil, fmt.Errorf("empty package name")
		}
		if !strings.Contains(module, "/") && !strings.Contains(module, ".") {
			resolved, err := resolveRegistryModule(module)
			if err != nil {
				return nil, fmt.Errorf("%v (or pass the full module path)", err)
			}
			module = resolved
		}
		if previous, exists := seen[module]; exists {
			if previous == version {
				continue
			}
			return nil, fmt.Errorf("package %s requested more than once with conflicting versions", module)
		}
		seen[module] = version
		packages = append(packages, packageInstall{module: module, requested: version})
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("need at least one package")
	}
	return packages, nil
}

// reviewInstalledCapabilities fires the capability review for a just-installed
// package when it's new or its required capabilities changed since wago-lock.json
// last recorded them, then persists the granted set to wago.json and the lock.
func reviewInstalledCapabilities(src, bin, module, version string) {
	id := strings.TrimPrefix(module, "github.com/")
	required, err := inspectRequiredCapabilities(bin, id)
	if err != nil {
		return // the package exposes no inspectable plugin, or inspect failed — skip
	}
	lock, err := readLock(src)
	if err != nil {
		fatal("add: reading plugin lock: %v", err)
	}
	entry, existed := lock.Packages[id]
	if existed && sameStringSet(entry.RequiredCapabilities, required) {
		entry.Version = version
		lock.Packages[id] = entry
		_ = writeLock(src, lock)
		return // already reviewed this exact capability set
	}
	if len(required) == 0 {
		lock.Packages[id] = lockEntry{
			Version:              version,
			RequiredCapabilities: []string{},
			Capabilities:         json.RawMessage("[]"),
			Config:               entry.Config,
		}
		_ = writeLock(src, lock)
		return
	}
	chosen, ok := reviewCapabilities(id, required, pluginGrants(src, id))
	if !ok {
		// Cancelled (esc): don't record, so the next install re-prompts.
		fmt.Printf("%s capability review skipped — set them anytime: wago plugin grant %s\n", dim("!"), id)
		return
	}
	capabilities, err := json.Marshal(chosen)
	if err != nil {
		fatal("add: recording capability grants: %v", err)
	}
	lock.Packages[id] = lockEntry{
		Version:              version,
		RequiredCapabilities: required,
		Capabilities:         capabilities,
		Config:               entry.Config,
	}
	_ = writeLock(src, lock)
	if len(chosen) == 0 {
		fmt.Printf("%s no capabilities granted — %s may not function; grant later: wago plugin grant %s\n", dim("!"), id, id)
	}
}

// inspectRequiredCapabilities runs the freshly-built binary to read the package's
// required capabilities (the current process usually doesn't have the package
// compiled in). Returns them sorted.
func inspectRequiredCapabilities(bin, id string) ([]string, error) {
	out, err := exec.Command(bin, "plugin", "inspect", id, "--json").Output()
	if err != nil {
		return nil, err
	}
	var rep struct {
		RequiresCapabilities []string `json:"requiresCapabilities"`
	}
	if err := json.Unmarshal(out, &rep); err != nil {
		return nil, err
	}
	sort.Strings(rep.RequiresCapabilities)
	return rep.RequiresCapabilities, nil
}

// pkgRemove drops a dependency from wago.json (a later build's `go mod tidy`
// prunes it from the build module).
func pkgRemove(name string, global bool) {
	src, err := depsSource(global)
	if err != nil {
		fatal("plugin remove: %v", err)
	}
	removed, module, err := removeProjectDep(src, name)
	if err != nil {
		fatal("plugin remove: %v", err)
	}
	if !removed {
		fatal("plugin remove: %q is not enabled in %s", name, projectManifestPath(src))
	}
	if buildDir, err := buildDirFor(global); err == nil {
		if _, statErr := os.Stat(buildDir); statErr == nil {
			deps, _ := projectDeps(src)
			_ = writeBuildMain(buildDir, deps)
		}
	}
	fmt.Printf("removed %s\n", dim(module))
}

// buildPlugins compiles (or reuses) the custom wago binary for deps, printing
// progress after an add transaction has already resolved the requested modules.
func buildPlugins(buildDir string, deps []string, o pkgOpts) (string, bool, error) {
	if o.verbose {
		fmt.Printf("%s\n", bold(fmt.Sprintf("building wago with %d plugin%s:", len(deps), plural(len(deps)))))
		for _, d := range deps {
			fmt.Printf("  %s\n", dim(d))
		}
	}
	bin, cached, err := ensureBuiltBinary(buildDir, deps, o.force, o.verbose)
	if err != nil {
		return "", false, err
	}
	if o.verbose {
		verb := "built"
		if cached {
			verb = "up to date"
		}
		fmt.Printf("%s %s  %s\n", cyan("✓"), verb, bin)
	}
	return bin, cached, nil
}

func installedModuleExactVersion(buildDir, module, requested string) string {
	if version, ok := currentModuleExactVersion(buildDir, module); ok {
		return version
	}
	if requested != "" {
		return requested
	}
	return "v0.0.0"
}

func currentModuleExactVersion(buildDir, module string) (string, bool) {
	cmd := exec.Command("go", "list", "-m", "-f={{.Version}}", module)
	cmd.Dir = buildDir
	if output, err := cmd.Output(); err == nil {
		if version := strings.TrimSpace(string(output)); version != "" {
			return version, true
		}
	}
	return "", false
}

func displayModuleVersion(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" || strings.HasPrefix(version, "0.0.0-") {
		return "0.0.0"
	}
	return version
}

// syncLockedPluginVersions restores the exact module versions captured in the
// lockfile before a generated build is reused or rebuilt. A manifest without a
// lockfile remains unresolved and is left for the Go module solver.
func syncLockedPluginVersions(buildDir, manifestDir string, verbose bool) (bool, error) {
	if err := ensureBuildModule(buildDir); err != nil {
		return false, err
	}
	m, err := readProjectMap(manifestDir)
	if err != nil {
		return false, err
	}
	requirements, err := projectPluginRequirements(m, manifestDir)
	if err != nil {
		return false, err
	}
	lock, err := readLock(manifestDir)
	if err != nil {
		return false, err
	}
	changed := false
	for _, requirement := range requirements {
		version := strings.TrimSpace(lock.Packages[requirement.ID].Version)
		if version == "" {
			if _, ok := currentModuleExactVersion(buildDir, requirement.Module); ok {
				continue
			}
			if _, err := os.Stat(filepath.Join(buildDir, "bin", "wago"+exeSuffix())); err == nil {
				continue
			}
			version = strings.TrimLeft(requirement.Constraint, "^~")
			if version == "*" {
				continue
			}
		}
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		if current, ok := currentModuleExactVersion(buildDir, requirement.Module); ok && current == version {
			continue
		}
		if err := goGetDep(buildDir, requirement.Module+"@"+version, verbose); err != nil {
			return false, fmt.Errorf("restore %s@%s: %w", requirement.Module, version, err)
		}
		changed = true
	}
	return changed, nil
}

func packageInstallDuration(elapsed time.Duration) string {
	if elapsed < time.Second {
		return fmt.Sprintf("%.1fms", float64(elapsed)/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.1fs", elapsed.Seconds())
}

func printPackageInstallSummary(out io.Writer, packages []packageInstall, elapsed time.Duration) {
	fmt.Fprintln(out)
	for _, pkg := range packages {
		name := strings.TrimPrefix(pkg.module, "github.com/")
		fmt.Fprintf(out, "%s %s@%s\n", cyan("+"), name, pkg.resolved)
	}
	fmt.Fprintf(out, "\n%d package%s installed [%s]\n",
		len(packages), plural(len(packages)), packageInstallDuration(elapsed))
}

// pkgUpdate updates plugins to their latest versions (go get -u) and
// rebuilds. With a target it updates just that plugin; otherwise all of them.
func pkgUpdate(target string, o pkgOpts) {
	buildDir, err := buildDirFor(o.global)
	if err != nil {
		fatal("plugin update: %v", err)
	}
	if err := ensureBuildModule(buildDir); err != nil {
		fatal("plugin update: %v", err)
	}
	src, err := depsSource(o.global)
	if err != nil {
		fatal("plugin update: %v", err)
	}
	deps, err := projectDeps(src)
	if err != nil {
		fatal("plugin update: %v", err)
	}
	if len(deps) == 0 {
		fatal("plugin update: no plugins to update (add one: wago add <module>)")
	}
	targets := deps
	if target != "" {
		if !strings.Contains(target, "/") && !strings.Contains(target, ".") {
			if m, err := resolveRegistryModule(target); err == nil {
				target = m
			}
		} else {
			target, _ = splitModuleVersion(normalizeModuleRef(target))
		}
		targets = []string{target}
	}
	for _, t := range targets {
		fmt.Printf("%s %s %s\n", dim("→ updating"), t, dim("(latest)"))
		if err := goUpdate(buildDir, t, o.verbose); err != nil {
			fatal("plugin update: %s: %v", t, err)
		}
		exact := installedModuleExactVersion(buildDir, t, "")
		if _, err := addProjectDep(src, t, "^"+displayModuleVersion(exact)); err != nil {
			fatal("plugin update: recording %s: %v", t, err)
		}
		lock, err := readLock(src)
		if err != nil {
			fatal("plugin update: %v", err)
		}
		id := strings.TrimPrefix(t, "github.com/")
		entry := lock.Packages[id]
		entry.Version = exact
		lock.Packages[id] = entry
		if err := writeLock(src, lock); err != nil {
			fatal("plugin update: %v", err)
		}
	}
	_ = writeBuildMain(buildDir, deps)
	bin, _, err := ensureBuiltBinary(buildDir, deps, true, o.verbose) // force rebuild after update
	if err != nil {
		fatal("plugin update: %v", err)
	}
	fmt.Printf("%s updated %d plugin%s  %s\n", cyan("✓"), len(targets), plural(len(targets)), bin)
}

// maybeReexecForPlugins transparently hands off to the active custom wago binary:
// the local project's build when it declares plugins, otherwise the global
// plugin build. It builds on demand after a cache miss, and is a no-op when
// nothing is declared or we're already running a plugin-built binary
// (WAGO_PLUGIN_ACTIVE). A build failure degrades to a warning so the current
// binary still runs.
func maybeReexecForPlugins() {
	if os.Getenv("WAGO_PLUGIN_ACTIVE") != "" {
		return
	}
	environment, err := resolvePluginEnvironment()
	if err != nil {
		fatal("plugins: %v", err)
	}
	if len(environment.dependencies) == 0 {
		return
	}
	changed, err := syncLockedPluginVersions(environment.buildDir, environment.manifestDir, false)
	if err != nil {
		fatal("plugins: %v", err)
	}
	bin, _, err := ensureBuiltBinary(environment.buildDir, environment.dependencies, changed, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s could not build plugins (%v); running without them\n", dim("wago:"), err)
		return
	}
	env := append(os.Environ(), "WAGO_PLUGIN_ACTIVE="+buildHash(environment.dependencies))
	if err := handoffPluginProcess(bin, append([]string{bin}, os.Args[1:]...), env); err != nil {
		fatal("plugins: exec %s: %v", bin, err)
	}
}

// truthyEnv reports whether env var k is set to a truthy value.
func truthyEnv(k string) bool {
	switch strings.ToLower(os.Getenv(k)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// splitModuleVersion splits a "module@version" spec; version is "" when absent.
// Only an '@' after the first character counts (so a bare module is untouched).
func splitModuleVersion(spec string) (module, version string) {
	if at := strings.LastIndexByte(spec, '@'); at > 0 {
		return spec[:at], spec[at+1:]
	}
	return spec, ""
}

// The consume side of `wago add` / `wago plugin`: declaring version-constrained
// plugins in wago.json, resolving them with Go modules, and building a custom
// runtime that has them compiled in.

package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/wago-org/wago/cli/internal/project"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/cli/manager/internal/registry"
)

// pkgOpts are the shared flags for the consume-side pkg commands.
type pkgOpts struct {
	global  bool // operate on the ~/.wago set instead of the project
	force   bool // ignore the build cache / fetch latest
	verbose bool // stream the underlying `go` output
}

func pkgAddMany(specs []string, o pkgOpts) {
	started := time.Now()
	progress := managerprogress.NewProgress(os.Stderr)
	if o.verbose {
		progress.DisableAnimation()
	}
	progress.Title("Installing packages")
	progress.Begin("Resolving packages")
	packages, err := ResolvePackages(specs, registry.ResolveModule)
	if err != nil {
		progress.Fail("Package resolution failed")
		fatal("add: %v", err)
	}

	buildDir, err := buildDirFor(o.global)
	if err != nil {
		progress.Fail("Package resolution failed")
		fatal("add: %v", err)
	}
	if err := pluginbuild.EnsureModule(buildDir); err != nil {
		progress.Fail("Package resolution failed")
		fatal("add: %v", err)
	}
	progress.Done(fmt.Sprintf("Resolved %d package%s", len(packages), plural(len(packages))))

	progress.Begin("Downloading packages")
	for _, pkg := range packages {
		getSpec := pkg.Module
		if pkg.Requested != "" {
			getSpec += "@" + pkg.Requested
		}
		var getErr error
		if o.force && pkg.Requested == "" {
			getErr = pluginbuild.Update(buildDir, pkg.Module, o.verbose)
		} else {
			getErr = pluginbuild.Get(buildDir, getSpec, o.verbose)
		}
		if getErr != nil {
			progress.Fail("Package download failed")
			if _, haveSrc := pluginbuild.SourceDir(); !haveSrc {
				fatal("add: fetching %s: %v\n  (during dev, set WAGO_SRC to a wago checkout so sibling plugins resolve locally)", getSpec, getErr)
			}
			fatal("add: fetching %s: %v", getSpec, getErr)
		}
	}
	progress.Done(fmt.Sprintf("Downloaded %d package%s", len(packages), plural(len(packages))))

	progress.Begin("Verifying packages")
	if err := pluginbuild.RunGo(buildDir, o.verbose, "mod", "verify"); err != nil {
		progress.Fail("Package verification failed")
		fatal("add: verifying packages: %v", err)
	}
	progress.Done("Verified package checksums")

	src, err := depsSource(o.global)
	if err != nil {
		progress.Fail("Package setup failed")
		fatal("add: %v", err)
	}
	if o.global {
		if err := os.MkdirAll(src, 0o755); err != nil {
			progress.Fail("Package setup failed")
			fatal("add: %v", err)
		}
	}
	for index := range packages {
		packages[index].Exact = installedModuleExactVersion(buildDir, packages[index].Module, packages[index].Requested)
		packages[index].Resolved = DisplayVersion(packages[index].Exact)
		if _, err := project.AddDependency(src, packages[index].Module, "^"+packages[index].Resolved); err != nil {
			progress.Fail("Package setup failed")
			fatal("add: %v", err)
		}
	}
	if !o.global {
		project.EnsureGitignore(".wago/")
	}
	deps, _ := project.Dependencies(src)
	_ = pluginbuild.WriteMain(buildDir, deps, pluginBuildConfig()) // keep the build module in sync

	// Rebuild the custom binary right away so the package is usable without a
	// separate build step.
	progress.Begin("Building packages")
	bin, cached, err := buildPlugins(buildDir, deps, o)
	if err != nil {
		progress.Fail("Package build failed")
		fatal("build: %v", err)
	}
	elapsed := time.Since(started)
	if cached {
		progress.Finish(fmt.Sprintf("Reused Wago build with %d package%s", len(packages), plural(len(packages))))
	} else {
		progress.Finish(fmt.Sprintf("Built Wago with %d package%s", len(packages), plural(len(packages))))
	}
	for _, pkg := range packages {
		registry.RecordInstall(pkg.Module, pkg.Resolved)
	}
	// Then review capabilities — on a first install, or when the package's
	// required capabilities have changed since the lockfile recorded them.
	for _, pkg := range packages {
		reviewInstalledCapabilities(src, bin, pkg.Module, pkg.Exact)
	}
	summary := make([]SummaryPackage, len(packages))
	for index, pkg := range packages {
		summary[index] = SummaryPackage{Module: pkg.Module, Version: pkg.Resolved}
	}
	PrintSummary(os.Stdout, summary, elapsed)
}

// reviewInstalledCapabilities fires the capability review for a just-installed
// package when it's new or its required capabilities changed since wago-lock.json
// last recorded them, then persists the grant in wago-lock.json.
func reviewInstalledCapabilities(src, bin, module, version string) {
	id := strings.TrimPrefix(module, "github.com/")
	required, err := inspectRequiredCapabilities(bin, id)
	if err != nil {
		return // the package exposes no inspectable plugin, or inspect failed — skip
	}
	lock, err := project.ReadLock(src)
	if err != nil {
		fatal("add: reading plugin lock: %v", err)
	}
	entry, existed := lock.Packages[id]
	if existed && project.SameStringSet(entry.RequiredCapabilities, required) {
		entry.Version = version
		lock.Packages[id] = entry
		_ = project.WriteLock(src, lock)
		return // already reviewed this exact capability set
	}
	if len(required) == 0 {
		lock.Packages[id] = project.LockEntry{
			Version:              version,
			RequiredCapabilities: []string{},
			Capabilities:         json.RawMessage("[]"),
			Config:               entry.Config,
		}
		_ = project.WriteLock(src, lock)
		return
	}
	chosen, ok := reviewCapabilities(id, required, project.Grants(src, id))
	if !ok {
		// Cancelled (esc): don't record, so the next install re-prompts.
		fmt.Printf("%s capability review skipped — set them anytime: wago plugin grant %s\n", dim("!"), id)
		return
	}
	capabilities, err := json.Marshal(chosen)
	if err != nil {
		fatal("add: recording capability grants: %v", err)
	}
	lock.Packages[id] = project.LockEntry{
		Version:              version,
		RequiredCapabilities: required,
		Capabilities:         capabilities,
		Config:               entry.Config,
	}
	_ = project.WriteLock(src, lock)
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
	removed, module, err := project.RemoveDependency(src, name)
	if err != nil {
		fatal("plugin remove: %v", err)
	}
	if !removed {
		fatal("plugin remove: %q is not enabled in %s", name, project.Path(src))
	}
	if buildDir, err := buildDirFor(global); err == nil {
		if _, statErr := os.Stat(buildDir); statErr == nil {
			deps, _ := project.Dependencies(src)
			_ = pluginbuild.WriteMain(buildDir, deps, pluginBuildConfig())
		}
	}
	fmt.Printf("removed %s\n", dim(module))
}

// buildPlugins compiles (or reuses) the custom wago binary for deps, printing
// progress. Shared by pkg build and the auto-rebuild after pkg install.
func buildPlugins(buildDir string, deps []string, o pkgOpts) (string, bool, error) {
	if o.verbose {
		fmt.Printf("%s\n", bold(fmt.Sprintf("building wago with %d plugin%s:", len(deps), plural(len(deps)))))
		for _, d := range deps {
			fmt.Printf("  %s\n", dim(d))
		}
	}
	bin, cached, err := pluginbuild.EnsureBinary(buildDir, deps, o.force, o.verbose, pluginBuildConfig())
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

func syncLockedPluginVersions(buildDir, manifestDir string, verbose bool) (bool, error) {
	if err := pluginbuild.EnsureModule(buildDir); err != nil {
		return false, err
	}
	requirements, err := project.Requirements(manifestDir)
	if err != nil {
		return false, err
	}
	lock, err := project.ReadLock(manifestDir)
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
			if _, err := os.Stat(pluginbuild.BinaryPath(buildDir)); err == nil {
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
		if err := pluginbuild.Get(buildDir, requirement.Module+"@"+version, verbose); err != nil {
			return false, fmt.Errorf("restore %s@%s: %w", requirement.Module, version, err)
		}
		changed = true
	}
	return changed, nil
}

// pkgUpdate updates plugins to their latest versions (go get -u) and
// rebuilds. With a target it updates just that plugin; otherwise all of them.
func pkgUpdate(target string, o pkgOpts) {
	buildDir, err := buildDirFor(o.global)
	if err != nil {
		fatal("plugin update: %v", err)
	}
	if err := pluginbuild.EnsureModule(buildDir); err != nil {
		fatal("plugin update: %v", err)
	}
	src, err := depsSource(o.global)
	if err != nil {
		fatal("plugin update: %v", err)
	}
	deps, err := project.Dependencies(src)
	if err != nil {
		fatal("plugin update: %v", err)
	}
	if len(deps) == 0 {
		fatal("plugin update: no plugins to update (add one: wago add <module>)")
	}
	targets := deps
	if target != "" {
		if !strings.Contains(target, "/") && !strings.Contains(target, ".") {
			if m, err := registry.ResolveModule(target); err == nil {
				target = m
			}
		} else {
			target, _ = splitModuleVersion(normalizeModuleRef(target))
		}
		targets = []string{target}
	}
	for _, t := range targets {
		fmt.Printf("%s %s %s\n", dim("→ updating"), t, dim("(latest)"))
		if err := pluginbuild.Update(buildDir, t, o.verbose); err != nil {
			fatal("plugin update: %s: %v", t, err)
		}
		exact := installedModuleExactVersion(buildDir, t, "")
		if _, err := project.AddDependency(src, t, "^"+DisplayVersion(exact)); err != nil {
			fatal("plugin update: recording %s: %v", t, err)
		}
		lock, err := project.ReadLock(src)
		if err != nil {
			fatal("plugin update: %v", err)
		}
		id := strings.TrimPrefix(t, "github.com/")
		entry := lock.Packages[id]
		entry.Version = exact
		lock.Packages[id] = entry
		if err := project.WriteLock(src, lock); err != nil {
			fatal("plugin update: %v", err)
		}
	}
	bin, _, err := pluginbuild.EnsureBinary(buildDir, deps, true, o.verbose, pluginBuildConfig()) // force rebuild after update
	if err != nil {
		fatal("plugin update: %v", err)
	}
	fmt.Printf("%s updated %d plugin%s  %s\n", cyan("✓"), len(targets), plural(len(targets)), bin)
}

// pluginRuntimeBinary resolves the local or global plugin-aware runtime for the
// current invocation, building it on a cache miss. The manager calls this before
// launching a runtime; runtime binaries never build or replace themselves.
func pluginRuntimeBinary() (string, bool, error) {
	environment, err := resolvePluginEnvironment()
	if err != nil {
		return "", false, err
	}
	if len(environment.dependencies) == 0 {
		return "", false, nil
	}
	changed, err := syncLockedPluginVersions(environment.buildDir, environment.manifestDir, false)
	if err != nil {
		return "", false, err
	}
	bin, _, err := pluginbuild.EnsureBinary(environment.buildDir, environment.dependencies, changed, false, pluginBuildConfig())
	if err != nil {
		return "", false, err
	}
	return bin, true, nil
}

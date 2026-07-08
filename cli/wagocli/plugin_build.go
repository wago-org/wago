// The consume side of `wago pkg`: declaring plugin dependencies in wago.json,
// pulling them in with `go get`, and building/running a custom wago that has them
// compiled in. The build machinery itself lives in wagomodule.go.
package wagocli

import (
	"fmt"
	"os"
	"strings"
)

// pkgAdd declares a plugin dependency: resolve its module path, `go get` it into
// the .wago build module, and record it in wago.json's dependencies.
func pkgAdd(modOrName string, global bool) {
	module, version := splitModuleVersion(modOrName)
	if !strings.Contains(module, "/") && !strings.Contains(module, ".") {
		// A bare name: resolve its Go module path from the registry.
		resolved, err := resolveRegistryModule(module)
		if err != nil {
			fatal("pkg add: %v (or pass the full module path)", err)
		}
		module = resolved
	}

	buildDir, err := buildDirFor(global)
	if err != nil {
		fatal("pkg add: %v", err)
	}
	if err := ensureBuildModule(buildDir); err != nil {
		fatal("pkg add: %v", err)
	}
	getSpec := module
	if version != "" {
		getSpec += "@" + version
	}
	fmt.Printf("%s %s\n", dim("go get"), getSpec)
	if err := goGetDep(buildDir, getSpec); err != nil {
		if _, haveSrc := wagoSourceDir(); !haveSrc {
			fatal("pkg add: go get %s: %v\n  (during dev, set WAGO_SRC to a wago checkout so sibling plugins resolve locally)", getSpec, err)
		}
		fatal("pkg add: go get %s: %v", getSpec, err)
	}

	src, _ := depsSource(global)
	newly, err := addProjectDep(src, module)
	if err != nil {
		fatal("pkg add: %v", err)
	}
	if !global {
		ensureGitignore(".wago/")
	}
	deps, _ := projectDeps(src)
	_ = writeBuildMain(buildDir, deps) // keep the build module in sync

	verb := "updated"
	if newly {
		verb = "added"
	}
	fmt.Printf("%s %s  %s\n", verb, cyan(deriveName(module)), dim(module))
	fmt.Printf("  %s\n", dim("run any module and wago rebuilds with it, or: wago pkg build"))
}

// pkgRemove drops a dependency from wago.json (a later build's `go mod tidy`
// prunes it from the build module).
func pkgRemove(name string, global bool) {
	src, err := depsSource(global)
	if err != nil {
		fatal("pkg remove: %v", err)
	}
	removed, module, err := removeProjectDep(src, name)
	if err != nil {
		fatal("pkg remove: %v", err)
	}
	if !removed {
		fatal("pkg remove: %q is not a dependency in %s", name, projectManifestPath(src))
	}
	if buildDir, err := buildDirFor(global); err == nil {
		if _, statErr := os.Stat(buildDir); statErr == nil {
			deps, _ := projectDeps(src)
			_ = writeBuildMain(buildDir, deps)
		}
	}
	fmt.Printf("removed %s  %s\n", cyan(deriveName(module)), dim(module))
}

// pkgList prints the declared plugin dependencies.
func pkgList(global bool) {
	src, err := depsSource(global)
	if err != nil {
		fatal("pkg list: %v", err)
	}
	deps, err := projectDeps(src)
	if err != nil {
		fatal("pkg list: %v", err)
	}
	if len(deps) == 0 {
		fmt.Println(dim("no dependencies declared; add one: wago pkg add <module>"))
		return
	}
	fmt.Printf("%s %s\n", bold("dependencies:"), dim(projectManifestPath(src)))
	for _, d := range deps {
		fmt.Printf("  %s  %s\n", cyan(deriveName(d)), dim(d))
	}
}

// pkgBuild builds (or reuses) the custom wago binary for the declared plugins.
func pkgBuild(global bool) {
	buildDir, err := buildDirFor(global)
	if err != nil {
		fatal("pkg build: %v", err)
	}
	src, _ := depsSource(global)
	deps, err := projectDeps(src)
	if err != nil {
		fatal("pkg build: %v", err)
	}
	if len(deps) == 0 {
		fatal("pkg build: no dependencies in %s (add one: wago pkg add <module>)", projectManifestPath(src))
	}
	fmt.Printf("%s\n", bold("building a custom wago binary with:"))
	for _, d := range deps {
		fmt.Printf("  %s  %s\n", cyan(deriveName(d)), dim(d))
	}
	bin, cached, err := ensureBuiltBinary(buildDir, deps)
	if err != nil {
		fatal("pkg build: %v", err)
	}
	verb := "built"
	if cached {
		verb = "up to date"
	}
	fmt.Printf("%s %s  %s\n", cyan("✓"), verb, bin)
}

// maybeReexecForPlugins transparently hands off to a custom wago binary with this
// project's plugins compiled in — building it once (then cache hits), so `wago
// run` "just works" with the declared dependencies. It's a no-op when nothing is
// declared or when we're already running a plugin-built binary (WAGO_PLUGIN_ACTIVE).
// A build failure degrades to a warning so the current binary still runs.
func maybeReexecForPlugins() {
	if os.Getenv("WAGO_PLUGIN_ACTIVE") != "" {
		return
	}
	buildDir, deps, _ := activePluginSet()
	if len(deps) == 0 {
		return
	}
	bin, _, err := ensureBuiltBinary(buildDir, deps)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s could not build plugins (%v); running without them\n", dim("wago:"), err)
		return
	}
	env := append(os.Environ(), "WAGO_PLUGIN_ACTIVE="+buildHash(deps))
	if err := execProcess(bin, append([]string{bin}, os.Args[1:]...), env); err != nil {
		fatal("plugins: exec %s: %v", bin, err)
	}
}

// activePluginSet resolves which plugin set `wago run` uses here, and a scope
// label ("bare"/"local"/"global"/"plain"). Order:
//   - WAGO_BARE       → none (run the plain engine)
//   - WAGO_GLOBAL     → the global set (~/.wago), ignoring the project
//   - local (default) → cwd wago.json + ./.wago, if it declares deps
//   - global          → ~/.wago, if it declares deps
//   - else            → plain (no plugins)
//
// Local and global never merge — the more specific one wins, like npx preferring
// a project's node_modules over a global install.
func activePluginSet() (dir string, deps []string, scope string) {
	if truthyEnv("WAGO_BARE") {
		return "", nil, "bare"
	}
	if !truthyEnv("WAGO_GLOBAL") {
		if ds, _ := projectDeps("."); len(ds) > 0 {
			if d, err := buildDirFor(false); err == nil {
				return d, ds, "local"
			}
		}
	}
	if d, err := buildDirFor(true); err == nil {
		if ds, _ := projectDeps(d); len(ds) > 0 {
			return d, ds, "global"
		}
	}
	return "", nil, "plain"
}

// truthyEnv reports whether env var k is set to a truthy value.
func truthyEnv(k string) bool {
	switch strings.ToLower(os.Getenv(k)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// pkgStatus prints which wago engine is running and which plugin set is active
// here — so global vs local (cwd) is never ambiguous.
func pkgStatus() {
	self, _ := os.Executable()
	augmented := os.Getenv("WAGO_PLUGIN_ACTIVE") != ""
	kind := "global engine"
	if augmented {
		kind = "custom build (plugins compiled in)"
	}
	fmt.Printf("%s %s  %s  %s\n", bold("wago"), "v"+versionString(), dim(self), dim(kind))

	dir, deps, scope := activePluginSet()
	switch scope {
	case "bare":
		fmt.Printf("active: %s  %s\n", cyan("bare"), dim("WAGO_BARE set — plugins disabled"))
	case "plain":
		fmt.Printf("active: %s  %s\n", cyan("plain"), dim("no plugins declared here"))
	default:
		fmt.Printf("active: %s  %s  %s\n", cyan(scope), dim(dir), dim(fmt.Sprintf("(%d)", len(deps))))
		for _, d := range deps {
			fmt.Printf("  %s  %s\n", cyan(deriveName(d)), dim(d))
		}
	}

	// Show the counterpart set for orientation.
	if local, _ := projectDeps("."); len(local) > 0 && scope != "local" {
		fmt.Printf("%s  %s\n", dim("local  (./wago.json):"), dim(fmt.Sprintf("%d declared — used by default here", len(local))))
	}
	if g, err := buildDirFor(true); err == nil {
		if gd, _ := projectDeps(g); len(gd) > 0 && scope != "global" {
			fmt.Printf("%s  %s\n", dim("global (~/.wago):"), dim(fmt.Sprintf("%d declared — use with --global or WAGO_GLOBAL=1", len(gd))))
		}
	}
	fmt.Printf("%s\n", dim("override: WAGO_BARE=1 (plain) · WAGO_GLOBAL=1 (global set)"))
}

// splitModuleVersion splits a "module@version" spec; version is "" when absent.
// Only an '@' after the first character counts (so a bare module is untouched).
func splitModuleVersion(spec string) (module, version string) {
	if at := strings.LastIndexByte(spec, '@'); at > 0 {
		return spec[:at], spec[at+1:]
	}
	return spec, ""
}

// ensureGitignore appends entry to ./.gitignore if not already present. Best
// effort — a missing .gitignore is created only inside a git working tree.
func ensureGitignore(entry string) {
	const name = ".gitignore"
	b, err := os.ReadFile(name)
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(line)
			if t == entry || t == strings.TrimRight(entry, "/") {
				return
			}
		}
		f, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
			_, _ = f.WriteString("\n")
		}
		_, _ = f.WriteString(entry + "\n")
		return
	}
	if _, err := os.Stat(".git"); err == nil {
		_ = os.WriteFile(name, []byte(entry+"\n"), 0o644)
	}
}

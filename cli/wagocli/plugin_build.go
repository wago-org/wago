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
	buildDir, deps := activePluginSet()
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

// activePluginSet returns the build dir and dependency set that apply to `wago
// run` here: the local project (cwd wago.json + ./.wago) if it declares any deps,
// else the global set (~/.wago). Local takes precedence — no merging.
func activePluginSet() (string, []string) {
	if deps, _ := projectDeps("."); len(deps) > 0 {
		if dir, err := buildDirFor(false); err == nil {
			return dir, deps
		}
	}
	if dir, err := buildDirFor(true); err == nil {
		if deps, _ := projectDeps(dir); len(deps) > 0 {
			return dir, deps
		}
	}
	return "", nil
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

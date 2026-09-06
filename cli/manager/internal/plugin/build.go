package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/cli/manager/internal/registry"
)

type pkgOpts struct {
	ctx             context.Context
	global          bool
	force           bool
	verbose         bool
	authorities     []string
	grantAll        bool
	denyAll         bool
	acceptContracts bool
	scopes          map[string]map[string]project.AuthorityScope
}

func pkgAddMany(specs []string, options pkgOpts) {
	started := time.Now()
	progress := managerprogress.NewProgress(os.Stderr)
	if options.verbose {
		progress.DisableAnimation()
	}
	progress.Title("Installing plugins")
	progress.Begin("Fetching plugins")
	selection := capturePluginRuntime()
	src, err := selection.depsSource(options.global)
	if err != nil {
		progress.Fail("Plugin fetch failed")
		fatal("add: %v", err)
	}
	buildDir, err := selection.buildDirFor(options.global)
	if err != nil {
		progress.Fail("Plugin fetch failed")
		fatal("add: %v", err)
	}
	if !automation.NoInput() {
		prompts, err := findPackageInstallPrompts(pluginContext(options.ctx), specs)
		if err != nil {
			progress.Fail("Plugin fetch failed")
			fatal("add: %v", err)
		}
		if len(prompts) != 0 {
			progress.Finish("Fetched package details")
			specs, err = reviewPackageInstallChoices(specs, prompts)
			if err != nil {
				progress.Fail("Plugin install cancelled")
				fatal("add: %v", err)
			}
			progress.Begin("Fetching selected plugins")
		}
	}
	var installedLock project.LockDocument
	err = withPluginMutationLock(pluginContext(options.ctx), src, func(mutation *project.Mutation) error {
		manifest, err := mutation.ReadManifest()
		if err != nil {
			return err
		}
		for _, spec := range specs {
			id, constraint, err := parsePluginSpec(spec)
			if err != nil {
				return err
			}
			if _, err := project.SetRequirement(manifest, id, constraint); err != nil {
				return err
			}
		}
		roots, err := project.RequirementsFromManifest(manifest)
		if err != nil {
			return err
		}
		previous, err := mutation.ReadLock()
		if err != nil {
			return err
		}
		plan, err := ResolveCatalogGraph(pluginContext(options.ctx), defaultCatalog(), roots, previous)
		if err != nil {
			return err
		}
		progress.Finish("Fetched plugins")
		progress.Title("Checking permissions")
		reviewed, err := reviewResolvedPluginPlan(plan, options)
		if err != nil {
			return err
		}
		printPluginPlanWarnings(reviewed.Warnings)
		progress.Finish("Permissions checked")
		progress.Begin("Building plugin runtime")
		if err := stageAndPublishLockedState(mutation, src, buildDir, manifest, reviewed.Lock, options.verbose, selection.config()); err != nil {
			return err
		}
		installedLock = reviewed.Lock
		return nil
	})
	if err != nil {
		progress.Fail("Plugin install failed")
		fatal("add: %v", err)
	}
	if !options.global {
		project.EnsureGitignore(".wago/")
	}
	reportCompletedPluginInstalls(context.WithoutCancel(pluginContext(options.ctx)), specs, installedLock, registry.RecordInstallContext)
	progress.Finish(fmt.Sprintf("Installed %d plugin%s in %s", len(specs), plural(len(specs)), time.Since(started).Round(time.Millisecond)))
}

func reportCompletedPluginInstalls(ctx context.Context, specs []string, lock project.LockDocument, record func(context.Context, string, string)) {
	sources := make(map[string]project.PluginSource, len(specs))
	seenPlugins := make(map[string]struct{}, len(lock.Plugins))
	var visit func(string)
	visit = func(id string) {
		if _, seen := seenPlugins[id]; seen {
			return
		}
		seenPlugins[id] = struct{}{}
		entry, ok := lock.Plugins[id]
		if !ok {
			return
		}
		if entry.Source.Module != "" && entry.Source.Version != "" {
			sources[entry.Source.Module] = entry.Source
		}
		dependencies := make([]string, 0, len(entry.Dependencies))
		for dependency := range entry.Dependencies {
			dependencies = append(dependencies, dependency)
		}
		for _, binding := range entry.Bindings {
			dependencies = append(dependencies, binding.Providers...)
		}
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			visit(dependency)
		}
	}
	for _, spec := range specs {
		id, _, err := parsePluginSpec(spec)
		if err != nil {
			continue
		}
		visit(id)
	}
	modules := make([]string, 0, len(sources))
	for module := range sources {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	for _, module := range modules {
		source := sources[module]
		record(ctx, source.Module, source.Version)
	}
}

func pkgRemove(name string, options pkgOpts) {
	selection := capturePluginRuntime()
	src, err := selection.depsSource(options.global)
	if err != nil {
		fatal("plugin remove: %v", err)
	}
	buildDir, err := selection.buildDirFor(options.global)
	if err != nil {
		fatal("plugin remove: %v", err)
	}
	id := project.ExpandGitHubPluginID(name)
	if err := project.ValidatePluginID(id); err != nil {
		fatal("plugin remove: %v", err)
	}
	err = withPluginMutationLock(pluginContext(options.ctx), src, func(mutation *project.Mutation) error {
		manifest, err := mutation.ReadManifest()
		if err != nil {
			return err
		}
		removed, err := project.DeleteRequirement(manifest, id)
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("%q is not a direct plugin in %s", id, project.DisplayPath(src))
		}
		roots, err := project.RequirementsFromManifest(manifest)
		if err != nil {
			return err
		}
		previous, err := mutation.ReadLock()
		if err != nil {
			return err
		}
		lock := project.NewLockDocument()
		if len(roots) != 0 {
			plan, err := ResolveCatalogGraph(pluginContext(options.ctx), defaultCatalog(), roots, previous)
			if err != nil {
				return err
			}
			reviewed, err := reviewRemovalResolution(plan, options)
			if err != nil {
				return err
			}
			printPluginPlanWarnings(reviewed.Warnings)
			lock = reviewed.Lock
		}
		return stageAndPublishLockedState(mutation, src, buildDir, manifest, lock, false, selection.config())
	})
	if err != nil {
		fatal("plugin remove: %v", err)
	}
	fmt.Printf("removed %s\n", dim(id))
}

func reviewRemovalResolution(plan ResolutionPlan, options pkgOpts) (reviewedPluginPlan, error) {
	if len(plan.Reviews) != 0 {
		return reviewedPluginPlan{}, fmt.Errorf("removal changes authority requests; run `wago plugin update` to review the new graph")
	}
	return reviewResolvedPluginPlan(plan, options)
}

func pkgUpdate(target string, options pkgOpts) {
	selection := capturePluginRuntime()
	src, err := selection.depsSource(options.global)
	if err != nil {
		fatal("plugin update: %v", err)
	}
	buildDir, err := selection.buildDirFor(options.global)
	if err != nil {
		fatal("plugin update: %v", err)
	}
	target = project.ExpandGitHubPluginID(target)
	err = withPluginMutationLock(pluginContext(options.ctx), src, func(mutation *project.Mutation) error {
		manifest, err := mutation.ReadManifest()
		if err != nil {
			return err
		}
		roots, err := project.RequirementsFromManifest(manifest)
		if err != nil {
			return err
		}
		if target != "" {
			if err := project.ValidatePluginID(target); err != nil {
				return err
			}
			found := false
			for _, root := range roots {
				found = found || root.ID == target
			}
			if !found {
				return fmt.Errorf("%q is not a direct plugin", target)
			}
		}
		previous, err := mutation.ReadLock()
		if err != nil {
			return err
		}
		plan, err := ResolveCatalogGraph(pluginContext(options.ctx), defaultCatalog(), roots, previous)
		if err != nil {
			return err
		}
		reviewed, err := reviewResolvedPluginPlan(plan, options)
		if err != nil {
			return err
		}
		printPluginPlanWarnings(reviewed.Warnings)
		return stageAndPublishLockedState(mutation, src, buildDir, manifest, reviewed.Lock, options.verbose, selection.config())
	})
	if err != nil {
		fatal("plugin update: %v", err)
	}
	fmt.Printf("%s updated the complete plugin graph\n", cyan("✓"))
}

func stageAndPublishLockedState(mutation *project.Mutation, manifestDir, buildDir string, manifest map[string]any, lock project.LockDocument, verbose bool, config pluginbuild.Config) error {
	manifestData, err := project.EncodeManifest(manifest)
	if err != nil {
		return err
	}
	lockData, err := project.EncodeLock(lock)
	if err != nil {
		return err
	}
	input, err := pluginbuild.InputFromLock(lock)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(buildDir), 0o755); err != nil {
		return err
	}
	staged, err := os.MkdirTemp(filepath.Dir(buildDir), ".wago-plugin-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	if err := pluginbuild.EnsureModule(staged); err != nil {
		return err
	}
	if err := pluginbuild.RejectLockedSourceReplacements(staged, input.Sources); err != nil {
		return err
	}
	for _, source := range input.Sources {
		if err := pluginbuild.Get(staged, source.Module+"@"+source.Version, verbose); err != nil {
			return fmt.Errorf("fetch %s@%s: %w", source.Module, source.Version, err)
		}
	}
	if err := pluginbuild.RunGo(staged, verbose, "mod", "verify"); err != nil {
		return fmt.Errorf("verify plugin checksums: %w", err)
	}
	if err := verifySourceChecksums(staged, input.Sources); err != nil {
		return err
	}
	bin, _, err := pluginbuild.EnsureBinary(staged, input, true, verbose, config)
	if err != nil {
		return err
	}
	if err := verifyStagedRuntime(bin); err != nil {
		return err
	}
	return publishPluginTransaction(mutation, buildDir, staged, manifestData, lockData)
}

func verifyStagedRuntime(binary string) error {
	command := exec.Command(binary)
	command.Env = append(os.Environ(), "WAGO_INTERNAL_VALIDATE_PLUGIN_SET=1")
	automation.ConfigureCommand(command)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify staged plugin runtime: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if len(strings.TrimSpace(string(output))) != 0 {
		return fmt.Errorf("verify staged plugin runtime produced unexpected output: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func verifySourceChecksums(buildDir string, sources []project.PluginSource) error {
	// Reconcile the generated module before listing it. Newer Go toolchains can
	// require a harmless go.mod normalization (for example, `go 1.22` to
	// `go 1.22.0`) before they will report its selected modules.
	command := exec.Command("go", "list", "-mod=mod", "-m", "-json", "all")
	command.Dir = buildDir
	command.Env = appendEnvironmentValue(os.Environ(), "GOWORK", "off")
	automation.ConfigureCommand(command)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read selected module checksums: %w: %s", err, strings.TrimSpace(string(output)))
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	selected := map[string]project.PluginSource{}
	for {
		var module struct{ Path, Version, Sum string }
		if err := decoder.Decode(&module); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if module.Path != "" {
			selected[module.Path] = project.PluginSource{Module: module.Path, Version: module.Version, Checksum: module.Sum}
		}
	}
	for _, source := range sources {
		// The generated runtime deliberately links the active Wago checkout during
		// core development. Its complete local tree is part of the build hash; it
		// is not a downloaded plugin artifact and therefore has no module h1.
		if source.Module == "github.com/wago-org/wago" {
			if _, local := pluginbuild.SourceDir(); local {
				continue
			}
		}
		got, ok := selected[source.Module]
		if !ok || got.Version != source.Version {
			return fmt.Errorf("locked source %s@%s is not the selected module version %s@%s", source.Module, source.Version, got.Module, got.Version)
		}
		download := exec.Command("go", "mod", "download", "-json", source.Module+"@"+source.Version)
		download.Dir = buildDir
		download.Env = appendEnvironmentValue(os.Environ(), "GOWORK", "off")
		automation.ConfigureCommand(download)
		data, err := download.Output()
		if err != nil {
			return fmt.Errorf("download locked source %s@%s: %w", source.Module, source.Version, err)
		}
		var artifact struct {
			Path, Version, Sum string
			Error              string
		}
		if err := json.Unmarshal(data, &artifact); err != nil {
			return fmt.Errorf("decode locked source %s@%s: %w", source.Module, source.Version, err)
		}
		if artifact.Error != "" {
			return fmt.Errorf("download locked source %s@%s: %s", source.Module, source.Version, artifact.Error)
		}
		if artifact.Path != source.Module || artifact.Version != source.Version || artifact.Sum != source.Checksum {
			return fmt.Errorf("locked source %s@%s checksum %s does not match downloaded module %s@%s checksum %s", source.Module, source.Version, source.Checksum, artifact.Path, artifact.Version, artifact.Sum)
		}
	}
	return nil
}

func appendEnvironmentValue(environment []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func syncLockedPluginVersions(buildDir, manifestDir string, verbose bool) (bool, error) {
	requirements, err := project.Requirements(manifestDir)
	if err != nil {
		return false, err
	}
	lock, err := project.ReadLock(manifestDir)
	if err != nil {
		return false, err
	}
	if err := project.ValidateLockedResolution(requirements, lock); err != nil {
		return false, err
	}
	input, err := pluginbuild.InputFromLock(lock)
	if err != nil {
		return false, err
	}
	// Reject before reconciliation so an existing replacement is reported
	// instead of being silently removed and treated as an unchanged exact pin.
	if err := pluginbuild.RejectLockedSourceReplacements(buildDir, input.Sources); err != nil {
		return false, err
	}
	if err := pluginbuild.EnsureModule(buildDir); err != nil {
		return false, err
	}
	if err := pluginbuild.RejectLockedSourceReplacements(buildDir, input.Sources); err != nil {
		return false, err
	}
	changed := false
	for _, source := range input.Sources {
		if current, ok := pluginbuild.ModuleVersion(buildDir, source.Module); ok && current == source.Version {
			continue
		}
		if err := pluginbuild.Get(buildDir, source.Module+"@"+source.Version, verbose); err != nil {
			return false, fmt.Errorf("restore %s@%s: %w", source.Module, source.Version, err)
		}
		changed = true
	}
	// Prove the selected version and h1 on every path, including a binary-cache
	// hit. Matching go.mod versions alone do not establish the immutable source
	// identity recorded by the lock.
	if err := verifySourceChecksums(buildDir, input.Sources); err != nil {
		return false, err
	}
	return changed, nil
}

func pluginRuntimeBinary() (string, bool, error) {
	environment, err := resolvePluginEnvironment()
	if err != nil {
		return "", false, err
	}
	if len(environment.dependencies) == 0 {
		return "", false, nil
	}
	lock, err := project.ReadLock(environment.manifestDir)
	if err != nil {
		return "", false, err
	}
	requirements, err := project.Requirements(environment.manifestDir)
	if err != nil {
		return "", false, err
	}
	if err := project.ValidateLockedResolution(requirements, lock); err != nil {
		return "", false, err
	}
	input, err := pluginbuild.InputFromLock(lock)
	if err != nil {
		return "", false, err
	}
	changed, err := syncLockedPluginVersions(environment.buildDir, environment.manifestDir, false)
	if err != nil {
		return "", false, err
	}
	bin, _, err := pluginbuild.EnsureBinary(environment.buildDir, input, changed, false, environment.selection.config())
	return bin, err == nil, err
}

func parsePluginSpec(spec string) (string, string, error) {
	spec = strings.TrimSpace(spec)
	id, constraint := splitPluginSpec(spec)
	id = project.ExpandGitHubPluginID(id)
	if err := project.ValidatePluginID(id); err != nil {
		return "", "", err
	}
	if constraint == "" {
		constraint = "*"
	}
	if err := project.ValidateConstraint(constraint); err != nil {
		return "", "", fmt.Errorf("plugin %s: %w", id, err)
	}
	return id, constraint, nil
}

func splitPluginSpec(spec string) (id, constraint string) {
	if index := strings.LastIndexByte(spec, '@'); index > 0 {
		return spec[:index], spec[index+1:]
	}
	return spec, ""
}

func defaultCatalog() Catalog {
	return HTTPCatalog{BaseURL: registry.BaseURL()}
}

func pluginContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func sortedLockIDs(lock project.LockDocument) []string {
	ids := make([]string, 0, len(lock.Plugins))
	for id := range lock.Plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

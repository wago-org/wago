package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/ui"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
)

type MaintenanceRequest struct {
	Name          string
	Global, Local bool
	Verbose       bool
}

func Outdated(request MaintenanceRequest) {
	dir, _ := maintenanceSource(request)
	requirements, lock := maintenanceState(dir)
	type report struct {
		Plugin, Current, Constraint string
	}
	reports := make([]report, 0, len(requirements))
	for _, requirement := range requirements {
		entry, ok := lock.Plugins[requirement.ID]
		current := "unresolved"
		if ok {
			current = strings.TrimPrefix(entry.Source.Version, "v")
		}
		reports = append(reports, report{Plugin: requirement.ID, Current: current, Constraint: requirement.Constraint})
	}
	if automation.JSON() {
		ui.PrintJSON(reports)
		return
	}
	for _, report := range reports {
		fmt.Printf("%s  %s  %s\n", report.Plugin, report.Current, dim(report.Constraint))
	}
	fmt.Println(dim("run `wago plugin update` to solve against the current catalog"))
}

func Tree(request MaintenanceRequest) {
	dir, global := maintenanceSource(request)
	_, lock := maintenanceState(dir)
	scope := "local"
	if global {
		scope = "global"
	}
	type entry struct {
		Plugin       string            `json:"plugin"`
		Version      string            `json:"version"`
		Direct       bool              `json:"direct"`
		Dependencies map[string]string `json:"dependencies"`
	}
	entries := make([]entry, 0, len(lock.Plugins))
	for _, id := range sortedLockIDs(lock) {
		locked := lock.Plugins[id]
		entries = append(entries, entry{Plugin: id, Version: locked.Source.Version, Direct: locked.Direct, Dependencies: locked.Dependencies})
	}
	if automation.JSON() {
		ui.PrintJSON(map[string]any{"scope": scope, "plugins": entries})
		return
	}
	fmt.Printf("Plugins (%s)\n", scope)
	for _, entry := range entries {
		kind := "transitive"
		if entry.Direct {
			kind = "direct"
		}
		fmt.Printf("  %s@%s  %s\n", entry.Plugin, strings.TrimPrefix(entry.Version, "v"), dim(kind))
		for dependency, constraint := range entry.Dependencies {
			fmt.Printf("    -> %s %s\n", dependency, dim(constraint))
		}
	}
}

func Rebuild(request MaintenanceRequest) {
	dir, global := maintenanceSource(request)
	requirements, lock := maintenanceState(dir)
	if len(requirements) == 0 {
		fatal("plugin rebuild: no plugins enabled")
	}
	if err := project.ValidateLockedResolution(requirements, lock); err != nil {
		fatal("plugin rebuild: %v", err)
	}
	buildDir, err := buildDirFor(global)
	if err != nil {
		fatal("plugin rebuild: %v", err)
	}
	input, err := pluginbuild.InputFromLock(lock)
	if err != nil {
		fatal("plugin rebuild: %v", err)
	}
	if _, err := syncLockedPluginVersions(buildDir, dir, request.Verbose); err != nil {
		fatal("plugin rebuild: %v", err)
	}
	bin, _, err := pluginbuild.EnsureBinary(buildDir, input, true, request.Verbose, pluginBuildConfig())
	if err != nil {
		fatal("plugin rebuild: %v", err)
	}
	if err := verifyStagedRuntime(bin); err != nil {
		fatal("plugin rebuild: %v", err)
	}
	fmt.Printf("%s rebuilt Wago with %d plugin%s  %s\n", cyan("✓"), len(lock.Plugins), plural(len(lock.Plugins)), bin)
}

func maintenanceSource(request MaintenanceRequest) (string, bool) {
	if !request.Global && !request.Local {
		environment, err := resolvePluginEnvironment()
		if err != nil {
			fatal("plugin: %v", err)
		}
		if environment.scope == "bare" {
			fatal("plugin: no plugin scope selected")
		}
		return environment.manifestDir, environment.scope != "local"
	}
	global := mustMutationScope(request.Global, request.Local)
	dir, err := depsSource(global)
	if err != nil {
		fatal("plugin: %v", err)
	}
	return dir, global
}

func maintenanceState(dir string) ([]project.PluginRequirement, project.LockDocument) {
	requirements, err := project.Requirements(dir)
	if err != nil {
		fatal("plugin: %v", err)
	}
	lock, err := project.ReadLock(dir)
	if err != nil {
		fatal("plugin: %v", err)
	}
	return requirements, lock
}

// Retained for tests and external callers that reason about update decisions.
func pluginUpdateRequired(current, locked, latest string, force bool) bool {
	return force || current != latest || locked != latest
}

func latestModuleVersion(module, current string) (string, error) {
	return "", fmt.Errorf("module-level latest lookup was removed; resolve %s through the plugin catalog", module)
}

func pluginContextOrBackground(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

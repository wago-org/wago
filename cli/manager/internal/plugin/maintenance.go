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
	if len(reports) == 0 {
		fmt.Println("no plugins enabled")
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
	if len(entries) == 0 {
		fmt.Println("  no plugins enabled")
		return
	}
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
	selection := capturePluginRuntime()
	dir, global := selection.maintenanceSource(request)
	buildDir, err := selection.buildDirFor(global)
	if err != nil {
		fatal("plugin rebuild: %v", err)
	}
	var bin string
	pluginCount := 0
	err = withPluginRuntimeLock(context.Background(), dir, func(mutation *project.Mutation) error {
		manifest, err := mutation.ReadManifest()
		if err != nil {
			return err
		}
		requirements, err := project.RequirementsFromManifest(manifest)
		if err != nil {
			return err
		}
		if len(requirements) == 0 {
			return fmt.Errorf("no plugins enabled")
		}
		lock, err := mutation.ReadLock()
		if err != nil {
			return err
		}
		if err := project.ValidateLockedResolution(requirements, lock); err != nil {
			return err
		}
		input, err := pluginbuild.InputFromLock(lock)
		if err != nil {
			return err
		}
		if _, err := syncPluginBuildVersions(buildDir, input, request.Verbose); err != nil {
			return err
		}
		bin, _, err = pluginbuild.EnsureVerifiedBinaryContext(context.Background(), buildDir, input, true, request.Verbose, selection.config(), verifyStagedRuntimeContext)
		if err != nil {
			return err
		}
		pluginCount = len(lock.Plugins)
		return nil
	})
	if err != nil {
		fatal("plugin rebuild: %v", err)
	}
	fmt.Printf("%s rebuilt Wago with %d plugin%s  %s\n", cyan("✓"), pluginCount, plural(pluginCount), bin)
}

func maintenanceSource(request MaintenanceRequest) (string, bool) {
	return capturePluginRuntime().maintenanceSource(request)
}

func (selection pluginRuntimeSelection) maintenanceSource(request MaintenanceRequest) (string, bool) {
	if !request.Global && !request.Local {
		environment, err := selection.resolveEnvironment()
		if err != nil {
			fatal("plugin: %v", err)
		}
		if environment.scope == "bare" {
			fatal("plugin: no plugin scope selected")
		}
		return environment.manifestDir, environment.scope != "local"
	}
	global := mustMutationScope(request.Global, request.Local)
	dir, err := selection.depsSource(global)
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

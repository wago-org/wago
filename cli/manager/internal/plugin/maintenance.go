package plugin

import (
	"encoding/json"
	"fmt"
	"os/exec"
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
		Plugin  string `json:"plugin"`
		Current string `json:"current"`
		Latest  string `json:"latest"`
	}
	var reports []report
	found := false
	for _, requirement := range requirements {
		current := strings.TrimPrefix(lock.Packages[requirement.ID].Version, "v")
		latest, err := latestModuleVersion(requirement.Module, lock.Packages[requirement.ID].Version)
		if err != nil {
			fatal("plugin outdated: %s: %v", requirement.ID, err)
		}
		latest = strings.TrimPrefix(latest, "v")
		if latest == "" || latest == current {
			continue
		}
		found = true
		reports = append(reports, report{Plugin: requirement.ID, Current: current, Latest: latest})
		if automation.JSON() {
			continue
		}
		fmt.Printf("%s  %s -> %s\n", requirement.ID, current, latest)
	}
	if automation.JSON() {
		ui.PrintJSON(reports)
		return
	}
	if !found {
		fmt.Println(dim("all plugins are up to date"))
	}
}

func Tree(request MaintenanceRequest) {
	dir, global := maintenanceSource(request)
	requirements, lock := maintenanceState(dir)
	scope := "local"
	if global {
		scope = "global"
	}
	type entry struct {
		Plugin     string `json:"plugin"`
		Version    string `json:"version"`
		Constraint string `json:"constraint"`
	}
	entries := make([]entry, 0, len(requirements))
	if !automation.JSON() {
		fmt.Printf("Plugins (%s)\n", scope)
	}
	for _, requirement := range requirements {
		lockedVersion := strings.TrimSpace(lock.Packages[requirement.ID].Version)
		version := "unresolved"
		if lockedVersion != "" {
			version = DisplayVersion(lockedVersion)
		}
		if requirement.Constraint == "" {
			requirement.Constraint = "latest"
		}
		entries = append(entries, entry{Plugin: requirement.ID, Version: version, Constraint: requirement.Constraint})
		if automation.JSON() {
			continue
		}
		fmt.Printf("  %s@%s  %s\n", requirement.ID, version, dim(requirement.Constraint))
	}
	if automation.JSON() {
		ui.PrintJSON(map[string]any{"scope": scope, "plugins": entries})
	}
}

func Rebuild(request MaintenanceRequest) {
	dir, global := maintenanceSource(request)
	dependencies, err := project.Dependencies(dir)
	if err != nil {
		fatal("plugin rebuild: %v", err)
	}
	if len(dependencies) == 0 {
		fatal("plugin rebuild: no plugins enabled")
	}
	buildDir, err := buildDirFor(global)
	if err != nil {
		fatal("plugin rebuild: %v", err)
	}
	if _, err := syncLockedPluginVersions(buildDir, dir, request.Verbose); err != nil {
		fatal("plugin rebuild: %v", err)
	}
	if err := pluginbuild.WriteMain(buildDir, dependencies, pluginBuildConfig()); err != nil {
		fatal("plugin rebuild: %v", err)
	}
	bin, _, err := pluginbuild.EnsureBinary(buildDir, dependencies, true, request.Verbose, pluginBuildConfig())
	if err != nil {
		fatal("plugin rebuild: %v", err)
	}
	fmt.Printf("%s rebuilt Wago with %d plugin%s  %s\n", cyan("✓"), len(dependencies), plural(len(dependencies)), bin)
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

func latestModuleVersion(module, current string) (string, error) {
	target := module
	if current != "" {
		target += "@" + current
	}
	command := exec.Command("go", "list", "-m", "-u", "-json", target)
	automation.ConfigureCommand(command)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	var report struct {
		Version string
		Update  *struct{ Version string }
	}
	if err := json.Unmarshal(output, &report); err != nil {
		return "", err
	}
	if report.Update != nil {
		return report.Update.Version, nil
	}
	return report.Version, nil
}

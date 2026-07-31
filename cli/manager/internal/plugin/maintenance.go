package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
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
		fmt.Printf("%s  %s -> %s\n", requirement.ID, current, latest)
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
	fmt.Printf("Plugins (%s)\n", scope)
	for _, requirement := range requirements {
		lockedVersion := strings.TrimSpace(lock.Packages[requirement.ID].Version)
		version := "unresolved"
		if lockedVersion != "" {
			version = DisplayVersion(lockedVersion)
		}
		if requirement.Constraint == "" {
			requirement.Constraint = "latest"
		}
		fmt.Printf("  %s@%s  %s\n", requirement.ID, version, dim(requirement.Constraint))
	}
}

func Why(request MaintenanceRequest) {
	dir, _ := maintenanceSource(request)
	requirements, lock := maintenanceState(dir)
	id := strings.TrimPrefix(normalizeModuleRef(request.Name), "github.com/")
	for _, requirement := range requirements {
		if requirement.ID != id && requirement.Module != request.Name {
			continue
		}
		entry := lock.Packages[requirement.ID]
		fmt.Printf("%s is directly required by %s\n", requirement.ID, project.DisplayPath(dir))
		fmt.Printf("  constraint  %s\n", requirement.Constraint)
		resolved := "unresolved"
		if strings.TrimSpace(entry.Version) != "" {
			resolved = DisplayVersion(entry.Version)
		}
		fmt.Printf("  resolved    %s\n", resolved)
		fmt.Printf("  capabilities %d granted\n", grantedCount(entry.Capabilities))
		return
	}
	fatal("plugin why: %q is not enabled", request.Name)
}

func Verify(request MaintenanceRequest) {
	dir, global := maintenanceSource(request)
	requirements, lock := maintenanceState(dir)
	seen := make(map[string]bool, len(requirements))
	for _, requirement := range requirements {
		seen[requirement.ID] = true
		entry, ok := lock.Packages[requirement.ID]
		if !ok || strings.TrimSpace(entry.Version) == "" {
			fatal("plugin verify: %s is not pinned in %s", requirement.ID, project.LockPath(dir))
		}
		if len(entry.Capabilities) != 0 && !json.Valid(entry.Capabilities) {
			fatal("plugin verify: %s has invalid capability state", requirement.ID)
		}
	}
	for id := range lock.Packages {
		if !seen[id] {
			fatal("plugin verify: %s is locked but not declared in wago.json", id)
		}
	}
	buildDir, err := buildDirFor(global)
	if err != nil {
		fatal("plugin verify: %v", err)
	}
	if _, err := os.Stat(filepath.Join(buildDir, "go.mod")); err == nil {
		if err := pluginbuild.RunGo(buildDir, request.Verbose, "mod", "verify"); err != nil {
			fatal("plugin verify: %v", err)
		}
	}
	fmt.Printf("%s verified %d plugin%s and lockfile\n", cyan("✓"), len(requirements), plural(len(requirements)))
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

func grantedCount(raw json.RawMessage) int {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return len(list)
	}
	var object map[string]json.RawMessage
	_ = json.Unmarshal(raw, &object)
	return len(object)
}

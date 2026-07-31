// Package plugin owns plugin scope, dependency installation, capability review,
// custom-runtime builds, and invocation routing.
package plugin

import (
	"github.com/wago-org/wago/cli/internal/project"
)

type AddRequest struct {
	Modules        []string
	Global, Local  bool
	Force, Verbose bool
}

type MutationRequest struct {
	Name          string
	Global, Local bool
	Verbose       bool
}

var configuredManagerVersion = "0.0.0"

// ConfigureManagerVersion binds plugin artifacts to the manager's toolchain.
// The manager calls it once, before dispatching any command.
func ConfigureManagerVersion(version string) {
	if version != "" {
		configuredManagerVersion = version
	}
}

func managerVersion() string { return configuredManagerVersion }

func Add(request AddRequest) {
	pkgAddMany(request.Modules, pkgOpts{
		global:  mustMutationScope(request.Global, request.Local),
		force:   request.Force,
		verbose: request.Verbose,
	})
}

func Remove(request MutationRequest) {
	pkgRemove(normalizeModuleRef(request.Name), mustMutationScope(request.Global, request.Local))
}

func Grant(request MutationRequest) {
	pkgGrant(request.Name, mustMutationScope(request.Global, request.Local))
}

func Update(request MutationRequest) {
	pkgUpdate(normalizeModuleRef(request.Name), pkgOpts{
		global:  mustMutationScope(request.Global, request.Local),
		verbose: request.Verbose,
	})
}

func mustMutationScope(global, local bool) bool {
	useGlobal, err := project.MutationGlobal(global, local)
	if err != nil {
		fatal("plugin: %v", err)
	}
	return useGlobal
}

func RuntimeBinary() (string, bool, error) {
	return pluginRuntimeBinary()
}

func Select(global, local, bare bool) error {
	return project.SelectScope(global, local, bare)
}

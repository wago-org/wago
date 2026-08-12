// Package plugin owns the strict plugin graph, review, build, and invocation
// boundary for the Wago manager.
package plugin

import (
	"context"
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
)

type AddRequest struct {
	Context         context.Context
	Modules         []string
	Global, Local   bool
	Force, Verbose  bool
	Authorities     []string
	GrantAll        bool
	DenyAll         bool
	AcceptContracts bool
	Scopes          map[string]map[string]project.AuthorityScope
}

type MutationRequest struct {
	Context         context.Context
	Name            string
	Global, Local   bool
	Force           bool
	Verbose         bool
	Authorities     []string
	GrantAll        bool
	DenyAll         bool
	AcceptContracts bool
	Scopes          map[string]map[string]project.AuthorityScope
}

type StandaloneInputs struct{ Build pluginbuild.Input }

var configuredManagerVersion = "0.0.0"

func ConfigureManagerVersion(version string) {
	if version != "" {
		configuredManagerVersion = version
	}
}

func managerVersion() string { return configuredManagerVersion }

func Add(request AddRequest) {
	pkgAddMany(request.Modules, pkgOpts{
		ctx: request.Context, global: mustMutationScope(request.Global, request.Local), force: request.Force, verbose: request.Verbose,
		authorities: request.Authorities, grantAll: request.GrantAll, denyAll: request.DenyAll, acceptContracts: request.AcceptContracts,
		scopes: request.Scopes,
	})
}

func Remove(request MutationRequest) {
	pkgRemove(strings.TrimSpace(request.Name), pkgOpts{
		ctx: request.Context, global: mustMutationScope(request.Global, request.Local),
		acceptContracts: request.AcceptContracts,
	})
}

func Grant(request MutationRequest) {
	pkgGrant(request.Name, mustMutationScope(request.Global, request.Local), request.Authorities, request.GrantAll, request.DenyAll, request.Scopes)
}

func Update(request MutationRequest) {
	pkgUpdate(strings.TrimSpace(request.Name), pkgOpts{
		ctx: request.Context, global: mustMutationScope(request.Global, request.Local), force: request.Force, verbose: request.Verbose,
		authorities: request.Authorities, grantAll: request.GrantAll, denyAll: request.DenyAll,
		acceptContracts: request.AcceptContracts, scopes: request.Scopes,
	})
}

func CheckOutdated(request MaintenanceRequest)  { Outdated(request) }
func ShowTree(request MaintenanceRequest)       { Tree(request) }
func RebuildRuntime(request MaintenanceRequest) { Rebuild(request) }

func mustMutationScope(global, local bool) bool {
	useGlobal, err := project.MutationGlobal(global, local)
	if err != nil {
		fatal("plugin: %v", err)
	}
	return useGlobal
}

func RuntimeBinary() (string, bool, error) { return pluginRuntimeBinary() }

func PrepareStandalone(buildDir string, verbose bool) (StandaloneInputs, error) {
	environment, err := resolvePluginEnvironment()
	if err != nil {
		return StandaloneInputs{}, err
	}
	if environment.scope == "bare" || len(environment.dependencies) == 0 {
		return StandaloneInputs{Build: pluginbuild.Input{}}, pluginbuild.EnsureModule(buildDir)
	}
	requirements, err := project.Requirements(environment.manifestDir)
	if err != nil {
		return StandaloneInputs{}, err
	}
	lock, err := project.ReadLock(environment.manifestDir)
	if err != nil {
		return StandaloneInputs{}, err
	}
	if err := project.ValidateLockedResolution(requirements, lock); err != nil {
		return StandaloneInputs{}, err
	}
	if err := pluginbuild.EnsureModule(buildDir); err != nil {
		return StandaloneInputs{}, err
	}
	input, err := pluginbuild.InputFromLock(lock)
	if err != nil {
		return StandaloneInputs{}, err
	}
	for _, source := range input.Sources {
		if err := pluginbuild.Get(buildDir, source.Module+"@"+source.Version, verbose); err != nil {
			return StandaloneInputs{}, err
		}
	}
	if err := pluginbuild.RunGo(buildDir, verbose, "mod", "verify"); err != nil {
		return StandaloneInputs{}, err
	}
	if err := verifySourceChecksums(buildDir, input.Sources); err != nil {
		return StandaloneInputs{}, err
	}
	return StandaloneInputs{Build: input}, nil
}

func Select(global, local, bare bool) error { return project.SelectScope(global, local, bare) }

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

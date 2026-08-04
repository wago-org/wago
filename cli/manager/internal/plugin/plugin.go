// Package plugin owns plugin scope, dependency installation, capability review,
// custom-runtime builds, and invocation routing.
package plugin

import (
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
)

type AddRequest struct {
	Modules        []string
	Global, Local  bool
	Force, Verbose bool
	Capabilities   []string
	GrantAll       bool
	DenyAll        bool
}

type MutationRequest struct {
	Name          string
	Global, Local bool
	Force         bool
	Verbose       bool
	Capabilities  []string
	GrantAll      bool
	DenyAll       bool
}

type StandaloneInputs struct {
	Dependencies []string
	Plugins      []project.PluginIntent
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
		global:       mustMutationScope(request.Global, request.Local),
		force:        request.Force,
		verbose:      request.Verbose,
		capabilities: request.Capabilities,
		grantAll:     request.GrantAll,
		denyAll:      request.DenyAll,
	})
}

func Remove(request MutationRequest) {
	pkgRemove(normalizeModuleRef(request.Name), mustMutationScope(request.Global, request.Local))
}

func Grant(request MutationRequest) {
	pkgGrant(request.Name, mustMutationScope(request.Global, request.Local), request.Capabilities, request.GrantAll, request.DenyAll)
}

func Update(request MutationRequest) {
	pkgUpdate(normalizeModuleRef(request.Name), pkgOpts{
		global:  mustMutationScope(request.Global, request.Local),
		force:   request.Force,
		verbose: request.Verbose,
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

func RuntimeBinary() (string, bool, error) {
	return pluginRuntimeBinary()
}

// PrepareStandalone resolves the active plugin scope into an isolated build
// module and returns the configuration that must be baked into an executable.
func PrepareStandalone(buildDir string, verbose bool, extraPlugins string) (StandaloneInputs, error) {
	environment, err := resolvePluginEnvironment()
	if err != nil {
		return StandaloneInputs{}, err
	}
	if err := pluginbuild.EnsureModule(buildDir); err != nil {
		return StandaloneInputs{}, err
	}
	var plugins []project.PluginIntent
	if len(environment.dependencies) != 0 {
		if _, err := syncLockedPluginVersions(buildDir, environment.manifestDir, verbose); err != nil {
			return StandaloneInputs{}, err
		}
		plugins, err = project.PluginIntents(environment.manifestDir)
		if err != nil {
			return StandaloneInputs{}, err
		}
	}
	plugins = appendExtraPluginIntents(plugins, extraPlugins)
	return StandaloneInputs{
		Dependencies: append([]string(nil), environment.dependencies...),
		Plugins:      plugins,
	}, nil
}

func appendExtraPluginIntents(intents []project.PluginIntent, list string) []project.PluginIntent {
	selected := append([]project.PluginIntent(nil), intents...)
	have := make(map[string]bool, len(selected))
	for _, intent := range selected {
		have[strings.TrimPrefix(intent.Name, "github.com/")] = true
	}
	for _, value := range strings.Split(list, ",") {
		name := strings.TrimPrefix(strings.TrimSpace(value), "github.com/")
		if name == "" || have[name] {
			continue
		}
		have[name] = true
		selected = append(selected, project.PluginIntent{Name: name})
	}
	return selected
}

func Select(global, local, bare bool) error {
	return project.SelectScope(global, local, bare)
}

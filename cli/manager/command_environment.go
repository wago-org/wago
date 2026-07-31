package manager

import (
	"fmt"
	"os"

	authlogin "github.com/wago-org/wago/cli/manager/commands/auth/login"
	pluginadd "github.com/wago-org/wago/cli/manager/commands/plugin/add"
	"github.com/wago-org/wago/cli/manager/commands/plugin/deprecate"
	"github.com/wago-org/wago/cli/manager/commands/plugin/grant"
	"github.com/wago-org/wago/cli/manager/commands/plugin/publish"
	pluginremove "github.com/wago-org/wago/cli/manager/commands/plugin/remove"
	"github.com/wago-org/wago/cli/manager/commands/plugin/unpublish"
	pluginupdate "github.com/wago-org/wago/cli/manager/commands/plugin/update"
	versioninstall "github.com/wago-org/wago/cli/manager/commands/version/install"
	managerplugin "github.com/wago-org/wago/cli/manager/internal/plugin"
	"github.com/wago-org/wago/cli/manager/internal/registry"
	managerself "github.com/wago-org/wago/cli/manager/internal/self"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

// commandEnvironment adapts manager-owned domain modules to command packages.
// Parsing, validation, and command-level orchestration stay in command.go.
type commandEnvironment struct{}

func (commandEnvironment) Login(options authlogin.Options) {
	registry.Login(registry.LoginRequest{
		Link: options.Link, Code: options.Code, WithToken: options.WithToken, Token: options.Token,
	})
}

func (commandEnvironment) Logout() { registry.Logout() }
func (commandEnvironment) Whoami() { registry.Whoami() }

func (commandEnvironment) Add(options pluginadd.Options) {
	managerplugin.Add(managerplugin.AddRequest{
		Modules: options.Modules, Global: options.Global, Local: options.Local,
		Force: options.Force, Verbose: options.Verbose,
	})
}

func (commandEnvironment) Remove(options pluginremove.Options) {
	managerplugin.Remove(managerplugin.MutationRequest{Name: options.Name, Global: options.Global, Local: options.Local})
}

func (commandEnvironment) Grant(options grant.Options) {
	managerplugin.Grant(managerplugin.MutationRequest{Name: options.Name, Global: options.Global, Local: options.Local})
}

func (commandEnvironment) UpdatePlugins(options pluginupdate.Options) {
	managerplugin.Update(managerplugin.MutationRequest{
		Name: options.Module, Global: options.Global, Local: options.Local, Verbose: options.Verbose,
	})
}

func (commandEnvironment) Publish(options publish.Options) {
	registry.Publish(registry.PublishRequest{
		Manifest: options.Manifest, Commit: options.Commit, Notes: options.Notes,
		Category: options.Category, Tags: options.Tags,
	})
}

func (commandEnvironment) Unpublish(options unpublish.Options) {
	registry.Unpublish(registry.UnpublishRequest{Target: options.Target, Yes: options.Yes})
}

func (commandEnvironment) Deprecate(options deprecate.Options) {
	registry.Deprecate(registry.DeprecateRequest{
		Target: options.Target, Message: options.Message, Undo: options.Undo,
	})
}

func (commandEnvironment) SelectScope(global, local, bare bool) error {
	return managerplugin.Select(global, local, bare)
}

func (commandEnvironment) RuntimeBinary() (string, bool, error) {
	return managerplugin.RuntimeBinary()
}

func (commandEnvironment) ScopeLabel() string {
	return managerplugin.ScopeLabel()
}

func (commandEnvironment) dirs() wagopaths.Dirs {
	return wagopaths.DirsFor(versionString())
}

func (e commandEnvironment) toolchain() managerversion.Toolchain {
	return managerversion.Toolchain{Dirs: e.dirs()}
}

func (e commandEnvironment) List()    { e.toolchain().List() }
func (e commandEnvironment) Current() { e.toolchain().Current() }
func (e commandEnvironment) Which()   { e.toolchain().Which() }
func (e commandEnvironment) Switch(version, profile, build string) {
	e.toolchain().Switch(version, profile, build)
}
func (e commandEnvironment) InstallRequested(options versioninstall.Options) {
	e.toolchain().Install(managerversion.InstallRequest{
		Versions: options.Versions, Latest: options.Latest, Nightly: options.Nightly, Canary: options.Canary,
		Profile: options.Profile, Build: options.Build,
	})
}
func (e commandEnvironment) UpdateVersion(args []string, nightly, canary bool, profile, build string) {
	e.toolchain().Update(managerversion.UpdateRequest{
		Args: args, Nightly: nightly, Canary: canary, Profile: profile, Build: build,
	})
}
func (e commandEnvironment) UninstallVersions(versions []string) {
	e.toolchain().Uninstall(versions)
}

func (commandEnvironment) Update() {
	managerself.Update(versionString(), managerself.ExecutablePath())
}

func (commandEnvironment) RequestedMode(value string, yes bool) (string, bool) {
	mode, ok := managerself.RequestedMode(value, yes)
	return string(mode), ok
}

func (commandEnvironment) Cancelled() {
	fmt.Fprintln(os.Stdout, "Cancelled.")
}

func (commandEnvironment) UninstallSelf(mode string, yes bool) {
	managerself.Uninstall(
		wagopaths.DirsFor(versionString()),
		managerself.ExecutablePath(),
		managerself.Mode(mode),
		yes,
		os.Stdin,
		os.Stdout,
	)
}

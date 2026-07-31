package manager

import (
	"fmt"
	"os"
	"time"

	"github.com/wago-org/wago/cli/internal/project"
	authlogin "github.com/wago-org/wago/cli/manager/commands/auth/login"
	cacheclean "github.com/wago-org/wago/cli/manager/commands/cache/clean"
	cacheoptions "github.com/wago-org/wago/cli/manager/commands/cache/options"
	cacheprune "github.com/wago-org/wago/cli/manager/commands/cache/prune"
	configcompletions "github.com/wago-org/wago/cli/manager/commands/config/completions"
	pluginadd "github.com/wago-org/wago/cli/manager/commands/plugin/add"
	"github.com/wago-org/wago/cli/manager/commands/plugin/deprecate"
	"github.com/wago-org/wago/cli/manager/commands/plugin/grant"
	pluginoutdated "github.com/wago-org/wago/cli/manager/commands/plugin/outdated"
	"github.com/wago-org/wago/cli/manager/commands/plugin/publish"
	pluginrebuild "github.com/wago-org/wago/cli/manager/commands/plugin/rebuild"
	pluginremove "github.com/wago-org/wago/cli/manager/commands/plugin/remove"
	pluginTree "github.com/wago-org/wago/cli/manager/commands/plugin/tree"
	"github.com/wago-org/wago/cli/manager/commands/plugin/unpublish"
	pluginupdate "github.com/wago-org/wago/cli/manager/commands/plugin/update"
	updatecmd "github.com/wago-org/wago/cli/manager/commands/update"
	versioninstall "github.com/wago-org/wago/cli/manager/commands/version/install"
	managercache "github.com/wago-org/wago/cli/manager/internal/cache"
	managerconfig "github.com/wago-org/wago/cli/manager/internal/config"
	managerplugin "github.com/wago-org/wago/cli/manager/internal/plugin"
	"github.com/wago-org/wago/cli/manager/internal/registry"
	managerself "github.com/wago-org/wago/cli/manager/internal/self"
	managerstatus "github.com/wago-org/wago/cli/manager/internal/status"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

// commandEnvironment adapts manager-owned domain modules to command packages.
// Parsing, validation, and command-level orchestration stay in command.go.
type commandEnvironment struct{}

func (e commandEnvironment) Status() {
	report, err := managerstatus.Inspect(e.dirs(), versionString(), executablePath())
	if err != nil {
		fatal("status: %v", err)
	}
	managerstatus.Print(os.Stdout, report)
}

func (commandEnvironment) Completions(options configcompletions.Options) {
	if options.Install || options.Output != "" {
		path, err := managerconfig.InstallCompletion(options.Shell, options.Output, options.RC)
		if err != nil {
			fatal("config completions: %v", err)
		}
		fmt.Printf("%s\n", displayPath(path))
		return
	}
	script, err := managerconfig.Completion(options.Shell)
	if err != nil {
		fatal("config completions: %v", err)
	}
	fmt.Print(script)
}

func (e commandEnvironment) CacheDir() { fmt.Println(displayPath(managercache.DownloadDir(e.dirs()))) }
func cacheSelection(selection cacheoptions.Selection) managercache.Selection {
	return managercache.Selection{Downloads: selection.Downloads, Builds: selection.Builds}
}
func (e commandEnvironment) CacheSize(selection cacheoptions.Selection) {
	bytes, err := managercache.Size(managercache.Paths(e.dirs(), cacheSelection(selection)))
	if err != nil {
		fatal("cache size: %v", err)
	}
	fmt.Println(managercache.FormatBytes(bytes))
}
func (e commandEnvironment) CacheClean(options cacheclean.Options) {
	selection := cacheSelection(options.Selection)
	if !selection.Downloads && !selection.Builds {
		var submitted bool
		var err error
		selection, submitted, err = managercache.ChooseClean(e.dirs())
		if err != nil {
			fatal("cache clean: %v", err)
		}
		if !submitted {
			fmt.Println("Cancelled.")
			return
		}
		if !selection.Downloads && !selection.Builds {
			fmt.Println("No caches selected.")
			return
		}
	}
	result, err := managercache.Clean(e.dirs(), selection)
	if err != nil {
		fatal("cache clean: %v", err)
	}
	suffix := "s"
	if result.Removed == 1 {
		suffix = ""
	}
	fmt.Printf("Cleaned %d cache location%s (%s)\n", result.Removed, suffix, managercache.FormatBytes(result.Bytes))
}
func (e commandEnvironment) CachePrune(options cacheprune.Options) {
	result, err := managercache.Prune(e.dirs(), time.Duration(options.Days)*24*time.Hour)
	if err != nil {
		fatal("cache prune: %v", err)
	}
	noun := "entries"
	if result.Removed == 1 {
		noun = "entry"
	}
	fmt.Printf("Pruned %d old cache %s (%s)\n", result.Removed, noun, managercache.FormatBytes(result.Bytes))
}

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
		Capabilities: options.Capabilities, GrantAll: options.GrantAll, DenyAll: options.DenyAll,
	})
}

func (commandEnvironment) Remove(options pluginremove.Options) {
	managerplugin.Remove(managerplugin.MutationRequest{Name: options.Name, Global: options.Global, Local: options.Local})
}

func (commandEnvironment) Grant(options grant.Options) {
	managerplugin.Grant(managerplugin.MutationRequest{
		Name: options.Name, Global: options.Global, Local: options.Local,
		Capabilities: options.Capabilities, GrantAll: options.All, DenyAll: options.DenyAll,
	})
}

func (commandEnvironment) UpdatePlugins(options pluginupdate.Options) {
	managerplugin.Update(managerplugin.MutationRequest{
		Name: options.Module, Global: options.Global, Local: options.Local,
		Force: options.Force, Verbose: options.Verbose,
	})
}

func maintenanceRequest(name string, global, local, verbose bool) managerplugin.MaintenanceRequest {
	return managerplugin.MaintenanceRequest{Name: name, Global: global, Local: local, Verbose: verbose}
}
func (commandEnvironment) OutdatedPlugins(options pluginoutdated.Options) {
	managerplugin.CheckOutdated(maintenanceRequest("", options.Global, options.Local, false))
}
func (commandEnvironment) PluginTree(options pluginTree.Options) {
	managerplugin.ShowTree(maintenanceRequest("", options.Global, options.Local, false))
}
func (commandEnvironment) RebuildPlugins(options pluginrebuild.Options) {
	managerplugin.RebuildRuntime(maintenanceRequest("", options.Global, options.Local, options.Verbose))
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
		Use: options.Use,
	})
}
func (e commandEnvironment) UpdateVersion(args []string, nightly, canary, force bool, profile, build, use string) {
	e.toolchain().Update(managerversion.UpdateRequest{
		Args: args, Nightly: nightly, Canary: canary, Force: force, Profile: profile, Build: build, Use: use,
	})
}
func (e commandEnvironment) UninstallVersions(versions []string) {
	e.toolchain().Uninstall(versions)
}
func (e commandEnvironment) UninstallAllVersions() { e.toolchain().UninstallAll() }

func (commandEnvironment) Update() {
	managerself.Update(versionString(), managerself.ExecutablePath())
}

func (e commandEnvironment) UpdateEverything(options updatecmd.Options) {
	activeRuntime := managerversion.ActiveVersion(e.dirs())
	if options.Manager {
		managerself.Update(versionString(), managerself.ExecutablePath())
	}
	if options.Runtime {
		channel := options.Channel
		if channel == "" {
			channel = activeRuntime
		}
		if channel == "canary" || channel == "nightly" {
			e.toolchain().Update(managerversion.UpdateRequest{Args: []string{channel}, Profile: options.Profile, Build: options.Build, Use: options.Use})
		} else {
			fmt.Fprintln(os.Stdout, dim("runtime is pinned; skipping channel update"))
		}
	}
	if options.Plugins {
		global, local := options.Global, options.Local
		if !global && !local {
			if _, err := os.Stat(project.Path(".")); os.IsNotExist(err) {
				global = true
			}
		}
		source := "."
		if global {
			source = e.dirs().Data
		}
		dependencies, err := project.Dependencies(source)
		if err != nil {
			fatal("update: %v", err)
		}
		if len(dependencies) == 0 {
			fmt.Fprintln(os.Stdout, dim("no plugins enabled; skipping plugin update"))
		} else {
			managerplugin.Update(managerplugin.MutationRequest{Global: global, Local: local, Verbose: options.Verbose})
		}
	}
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

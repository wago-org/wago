package manager

import (
	"fmt"
	"os"
	"time"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/settings"
	"github.com/wago-org/wago/cli/internal/ui"
	authlogin "github.com/wago-org/wago/cli/manager/commands/auth/login"
	cacheclean "github.com/wago-org/wago/cli/manager/commands/cache/clean"
	cacheoptions "github.com/wago-org/wago/cli/manager/commands/cache/options"
	cacheprune "github.com/wago-org/wago/cli/manager/commands/cache/prune"
	configcompletions "github.com/wago-org/wago/cli/manager/commands/config/completions"
	configoptions "github.com/wago-org/wago/cli/manager/commands/config/options"
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
	if automation.DryRun() {
		automation.PrintPlan("configure shell completions", map[string]any{"shell": options.Shell, "install": options.Install, "output": options.Output, "rc": options.RC})
		return
	}
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

func (commandEnvironment) Configure(request configoptions.Request) {
	target, err := settings.Open(request.Global, request.Local)
	if err != nil {
		fatal("config: %v", err)
	}
	config := target.Config()
	scope, path := target.Scope(), displayPath(target.Path())
	switch request.Action {
	case configoptions.Interactive:
		updated, changed, err := managerconfig.Interactive(config, target.ResetBase(), scope, request.Experimental)
		if err != nil {
			fatal("config: %v", err)
		}
		if changed {
			if err := target.Replace(updated); err != nil {
				fatal("config: %v", err)
			}
			if automation.DryRun() {
				automation.PrintPlan("change Wago configuration", map[string]any{"scope": scope, "path": target.Path(), "settings": updated})
				return
			}
			if err := target.Save(); err != nil {
				fatal("config: %v", err)
			}
			fmt.Printf("%s Saved Wago %s configuration to %s\n", ui.Cyan("✓"), scope, path)
		}
	case configoptions.List:
		if automation.JSON() {
			output := map[string]any{"scope": scope, "path": target.Path(), "settings": config}
			if request.Experimental {
				output["experimental"] = settings.Experimental()
			}
			ui.PrintJSON(output)
			return
		}
		managerconfig.Print(os.Stdout, config, request.Experimental, scope, path)
	case configoptions.Get:
		value, err := target.Get(request.Key)
		if err != nil {
			fatal("config get: %v", err)
		}
		if automation.JSON() {
			ui.PrintJSON(map[string]any{"scope": scope, "key": settings.CanonicalKey(request.Key), "value": value})
			return
		}
		fmt.Println(value)
	case configoptions.Set:
		key := settings.CanonicalKey(request.Key)
		if err := target.Set(key, request.Value); err != nil {
			fatal("config set: %v", err)
		}
		if automation.DryRun() {
			automation.PrintPlan("change Wago default", map[string]any{"scope": scope, "path": target.Path(), "key": key, "value": request.Value})
			return
		}
		if err := target.Save(); err != nil {
			fatal("config set: %v", err)
		}
		value, _ := target.Get(key)
		if automation.JSON() {
			ui.PrintJSON(map[string]any{"scope": scope, "key": key, "value": value, "path": target.Path()})
			return
		}
		fmt.Printf("%s Set %s = %s (%s)\n", ui.Cyan("✓"), key, value, scope)
	case configoptions.Reset:
		if automation.DryRun() {
			automation.PrintPlan("reset Wago defaults", map[string]any{"scope": scope, "path": target.Path(), "key": request.Key, "all": request.All})
			return
		}
		if request.All {
			target.ResetAll()
		} else if err := target.Reset(request.Key); err != nil {
			fatal("config reset: %v", err)
		}
		if err := target.Save(); err != nil {
			fatal("config reset: %v", err)
		}
		if automation.JSON() {
			ui.PrintJSON(map[string]any{"scope": scope, "key": settings.CanonicalKey(request.Key), "all": request.All, "path": target.Path()})
			return
		}
		if request.All {
			if scope == settings.ScopeLocal {
				fmt.Printf("%s Cleared all local Wago overrides\n", ui.Cyan("✓"))
			} else {
				fmt.Printf("%s Restored all global Wago defaults\n", ui.Cyan("✓"))
			}
		} else {
			verb := "Restored"
			if scope == settings.ScopeLocal {
				verb = "Cleared override for"
			}
			fmt.Printf("%s %s %s\n", ui.Cyan("✓"), verb, settings.CanonicalKey(request.Key))
		}
	}
}

func (e commandEnvironment) CacheDir() {
	path := managercache.DownloadDir(e.dirs())
	if automation.JSON() {
		ui.PrintJSON(map[string]string{"path": path})
		return
	}
	fmt.Println(displayPath(path))
}
func cacheSelection(selection cacheoptions.Selection) managercache.Selection {
	return managercache.Selection{Downloads: selection.Downloads, Builds: selection.Builds}
}
func (e commandEnvironment) CacheSize(selection cacheoptions.Selection) {
	bytes, err := managercache.Size(managercache.Paths(e.dirs(), cacheSelection(selection)))
	if err != nil {
		fatal("cache size: %v", err)
	}
	if automation.JSON() {
		ui.PrintJSON(map[string]any{
			"bytes": bytes, "formatted": managercache.FormatBytes(bytes),
			"downloads": selection.Downloads, "builds": selection.Builds,
		})
		return
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
	if automation.DryRun() {
		automation.PrintPlan("clean cache", map[string]any{"downloads": selection.Downloads, "builds": selection.Builds})
		return
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
	if automation.DryRun() {
		automation.PrintPlan("prune cache", map[string]any{"minimumAgeDays": options.Days})
		return
	}
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
	if automation.DryRun() {
		method := "interactive"
		switch {
		case options.Link:
			method = "link"
		case options.Code:
			method = "code"
		case options.Token != "":
			method = "token"
		case options.WithToken:
			method = "stdin-token"
		}
		automation.PrintPlan("log in", map[string]any{"registry": "plugins.wago.sh", "method": method})
		return
	}
	registry.Login(registry.LoginRequest{
		Link: options.Link, Code: options.Code, WithToken: options.WithToken, Token: options.Token,
	})
}

func (commandEnvironment) Logout() {
	if automation.DryRun() {
		automation.PrintPlan("log out", map[string]any{"registry": "plugins.wago.sh"})
		return
	}
	registry.Logout()
}
func (commandEnvironment) Whoami() { registry.Whoami() }

func (commandEnvironment) Add(options pluginadd.Options) {
	requireUnlocked("add plugins")
	if automation.DryRun() {
		automation.PrintPlan("add plugins", map[string]any{"packages": options.Modules, "scope": scopeName(options.Global, options.Local), "force": options.Force})
		return
	}
	managerplugin.Add(managerplugin.AddRequest{
		Modules: options.Modules, Global: options.Global, Local: options.Local,
		Force: options.Force, Verbose: options.Verbose,
		Capabilities: options.Capabilities, GrantAll: options.GrantAll, DenyAll: options.DenyAll,
	})
}

func (commandEnvironment) Remove(options pluginremove.Options) {
	requireUnlocked("remove a plugin")
	if automation.DryRun() {
		automation.PrintPlan("remove plugin", map[string]any{"package": options.Name, "scope": scopeName(options.Global, options.Local)})
		return
	}
	managerplugin.Remove(managerplugin.MutationRequest{Name: options.Name, Global: options.Global, Local: options.Local})
}

func (commandEnvironment) Grant(options grant.Options) {
	requireUnlocked("change plugin grants")
	if automation.DryRun() {
		automation.PrintPlan("change plugin grants", map[string]any{"package": options.Name, "scope": scopeName(options.Global, options.Local), "capabilities": options.Capabilities, "all": options.All, "denyAll": options.DenyAll})
		return
	}
	managerplugin.Grant(managerplugin.MutationRequest{
		Name: options.Name, Global: options.Global, Local: options.Local,
		Capabilities: options.Capabilities, GrantAll: options.All, DenyAll: options.DenyAll,
	})
}

func (commandEnvironment) UpdatePlugins(options pluginupdate.Options) {
	requireUnlocked("update plugins")
	if automation.DryRun() {
		automation.PrintPlan("update plugins", map[string]any{"package": options.Module, "scope": scopeName(options.Global, options.Local), "force": options.Force})
		return
	}
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
	if automation.DryRun() {
		automation.PrintPlan("rebuild plugin runtime", map[string]any{"scope": scopeName(options.Global, options.Local)})
		return
	}
	managerplugin.RebuildRuntime(maintenanceRequest("", options.Global, options.Local, options.Verbose))
}

func (commandEnvironment) Publish(options publish.Options) {
	if automation.DryRun() {
		automation.PrintPlan("publish plugin", map[string]any{"manifest": options.Manifest, "commit": options.Commit, "category": options.Category, "tags": options.Tags})
		return
	}
	registry.Publish(registry.PublishRequest{
		Manifest: options.Manifest, Commit: options.Commit, Notes: options.Notes,
		Category: options.Category, Tags: options.Tags,
	})
}

func (commandEnvironment) Unpublish(options unpublish.Options) {
	if automation.DryRun() {
		automation.PrintPlan("unpublish plugin", map[string]any{"target": options.Target})
		return
	}
	registry.Unpublish(registry.UnpublishRequest{Target: options.Target, Yes: options.Yes})
}

func (commandEnvironment) Deprecate(options deprecate.Options) {
	if automation.DryRun() {
		automation.PrintPlan("change plugin deprecation", map[string]any{"target": options.Target, "message": options.Message, "undo": options.Undo})
		return
	}
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
	if automation.DryRun() {
		automation.PrintPlan("switch runtime", map[string]any{"version": version, "profile": profile, "build": build, "installIfMissing": true})
		return
	}
	e.toolchain().Switch(version, profile, build)
}
func (e commandEnvironment) InstallRequested(options versioninstall.Options) {
	if automation.DryRun() {
		automation.PrintPlan("install runtime", map[string]any{"versions": options.Versions, "latest": options.Latest, "nightly": options.Nightly, "canary": options.Canary, "profile": options.Profile, "build": options.Build, "use": options.Use})
		return
	}
	e.toolchain().Install(managerversion.InstallRequest{
		Versions: options.Versions, Latest: options.Latest, Nightly: options.Nightly, Canary: options.Canary,
		Profile: options.Profile, Build: options.Build,
		Use: options.Use,
	})
}
func (e commandEnvironment) UpdateVersion(args []string, nightly, canary, force bool, profile, build, use string) {
	if automation.DryRun() {
		automation.PrintPlan("update runtime", map[string]any{"versions": args, "nightly": nightly, "canary": canary, "force": force, "profile": profile, "build": build, "use": use})
		return
	}
	e.toolchain().Update(managerversion.UpdateRequest{
		Args: args, Nightly: nightly, Canary: canary, Force: force, Profile: profile, Build: build, Use: use,
	})
}
func (e commandEnvironment) UninstallVersions(versions []string) {
	if automation.DryRun() {
		automation.PrintPlan("uninstall runtimes", map[string]any{"versions": versions})
		return
	}
	e.toolchain().Uninstall(versions)
}
func (e commandEnvironment) UninstallAllVersions() {
	if automation.DryRun() {
		automation.PrintPlan("uninstall runtimes", map[string]any{"all": true})
		return
	}
	e.toolchain().UninstallAll()
}

func (commandEnvironment) Update(force bool) {
	if automation.DryRun() {
		automation.PrintPlan("update Wago", map[string]any{"component": "manager", "force": force})
		return
	}
	managerself.Update(versionString(), managerself.ExecutablePath(), force)
}

func (e commandEnvironment) UpdateEverything(options updatecmd.Options) {
	if options.Plugins {
		requireUnlocked("update plugins")
	}
	if automation.DryRun() {
		automation.PrintPlan("update Wago", map[string]any{"manager": options.Manager, "runtime": options.Runtime, "plugins": options.Plugins, "channel": options.Channel, "profile": options.Profile, "build": options.Build, "scope": scopeName(options.Global, options.Local), "force": options.Force, "use": options.Use})
		return
	}
	activeRuntime := managerversion.ActiveVersion(e.dirs())
	if options.Manager {
		managerself.Update(versionString(), managerself.ExecutablePath(), options.Force)
	}
	if options.Runtime {
		channel := options.Channel
		if channel == "" {
			channel = activeRuntime
		}
		if channel == "canary" || channel == "nightly" {
			e.toolchain().Update(managerversion.UpdateRequest{Args: []string{channel}, Profile: options.Profile, Build: options.Build, Use: options.Use, Force: options.Force})
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
			managerplugin.Update(managerplugin.MutationRequest{Global: global, Local: local, Verbose: options.Verbose, Force: options.Force})
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
	if automation.DryRun() {
		automation.PrintPlan("uninstall Wago", map[string]any{"mode": mode, "confirmed": yes})
		return
	}
	managerself.Uninstall(
		wagopaths.DirsFor(versionString()),
		managerself.ExecutablePath(),
		managerself.Mode(mode),
		yes,
		os.Stdin,
		os.Stdout,
	)
}

func scopeName(global, local bool) string {
	if global {
		return "global"
	}
	if local {
		return "local"
	}
	return "auto"
}

func requireUnlocked(action string) {
	if automation.Locked() && !automation.DryRun() {
		ui.Fatal("--locked prevents %s because it changes wago.json or wago-lock.json", action)
	}
}

//go:build !wago_manager && !wago_minimal

package wagocli

import (
	"sort"
	"strconv"
	"strings"

	"github.com/wago-org/wago"
)

func prepareRunnerInvocation(args []string) {
	if usesPluginRuntime(args) {
		applyInvocationPluginScope(args)
		maybeReexecForPlugins()
	}
}

func runProfileFlags() []Flag {
	return []Flag{
		{Name: "plugin", Arg: "<names>", Help: "comma-separated extra plugins to enable, on top of wago.json (see: wago plugin list)"},
		{Name: "local", Bool: true, Help: "use this project's plugins (default when wago.json exists)"},
		{Name: "global", Bool: true, Help: "use the shared user-wide plugins"},
		{Name: "bare", Bool: true, Help: "run without local or global plugins"},
	}
}

func prepareRunPlugins() { maybeReexecForPlugins() }

func loadRunRuntime(cfg *wago.RuntimeConfig, plugins string) *wago.Runtime {
	return loadPluginRuntime(cfg, plugins)
}

func applyInvocationPluginScope(args []string) {
	if len(args) == 0 {
		return
	}
	if args[0] == "plugin" || args[0] == "plugins" {
		if len(args) < 2 || (args[1] != "list" && args[1] != "ls" && args[1] != "inspect") {
			return
		}
		global, local := boolFlag(args[2:], "--global", "-g"), boolFlag(args[2:], "--local", "-l")
		if err := selectPluginScope(global, local, false); err != nil {
			fatal("plugin %s: %v", args[1], err)
		}
		return
	}
	if args[0] == "build" {
		global, local := boolFlag(args[1:], "--global"), boolFlag(args[1:], "--local")
		bare := boolFlag(args[1:], "--bare")
		if err := selectPluginScope(global, local, bare); err != nil {
			fatal("build: %v", err)
		}
		return
	}
	applyRunPluginScope(args)
}

func applyRunPluginScope(args []string) {
	if len(args) == 0 {
		return
	}
	if args[0] == "run" {
		args = args[1:]
	} else if !looksLikeRunTarget(args[0]) && !strings.HasPrefix(args[0], "-") {
		return
	}
	global, local, bare := false, false, false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" || arg == "-" || !strings.HasPrefix(arg, "-") {
			break
		}
		switch arg {
		case "--global":
			global = true
		case "--local":
			local = true
		case "--bare":
			bare = true
		case "--invoke", "-e", "--bounds", "--plugin":
			if index+1 < len(args) {
				index++
			}
		case "--parallel", "-p":
			if index+1 < len(args) {
				if _, err := strconv.Atoi(args[index+1]); err == nil {
					index++
				}
			}
		}
	}
	if err := selectPluginScope(global, local, bare); err != nil {
		fatal("run: %v", err)
	}
}

func boolFlag(args []string, names ...string) bool {
	for _, arg := range args {
		for _, name := range names {
			if arg == name {
				return true
			}
		}
	}
	return false
}

func versionPluginSummary() string {
	compiled := compiledPluginSummary()
	environment, err := resolvePluginEnvironment()
	if err != nil || len(environment.dependencies) == 0 {
		return compiled
	}
	configured := append([]string(nil), environment.dependencies...)
	sort.Strings(configured)
	summary := strings.Join(configured, ", ") + " (" + environment.scope + ")"
	if compiled != "none" {
		summary += "; compiled: " + compiled
	}
	return summary
}

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/plugins/log"
	"github.com/wago-org/wago/plugins/metrics"
	"github.com/wago-org/wago/plugins/timer"
)

// The built-in plugins compiled into this binary. Each is a small, dependency-
// free package; the Go linker only keeps what is imported, so a leaner binary can
// drop these imports (and heavier third-party plugins live in their own modules,
// wired in via a custom build).
func init() {
	wago.RegisterExtension("timer", func() wago.Extension { return timer.Ext() })
	wago.RegisterExtension("log", func() wago.Extension { return log.Ext() })
	wago.RegisterExtension("metrics", func() wago.Extension { return metrics.Ext() })
}

// pluginCmd dispatches `wago plugin <sub>`.
func pluginCmd(args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list", "ls":
		pluginList()
	case "install", "add", "uninstall", "remove", "update", "build":
		fatal("plugin %s: not yet implemented — third-party plugins are added by\n"+
			"building a custom wago binary that imports them (a manifest-driven\n"+
			"`wago plugin build` is planned). Built-in plugins are always available;\n"+
			"see `wago plugin list`.", sub)
	default:
		fatal("plugin: unknown subcommand %q (have: list)", sub)
	}
}

// pluginList prints the plugins compiled into this binary, with their id,
// version, and the capabilities they require.
func pluginList() {
	names := wago.RegisteredPluginNames()
	if len(names) == 0 {
		fmt.Println(dim("no plugins compiled into this binary"))
		return
	}
	fmt.Printf("%s\n", bold("plugins:"))
	for _, name := range names {
		ext, ok := wago.NewExtension(name)
		if !ok {
			continue
		}
		info := ext.Info()
		caps := pluginCapabilities(ext)
		line := fmt.Sprintf("  %s  %s %s", cyan(name), dim(info.ID), info.Version)
		if len(caps) > 0 {
			line += "  " + dim("caps: "+strings.Join(caps, ", "))
		}
		fmt.Println(line)
		if info.Description != "" {
			fmt.Printf("      %s\n", dim(info.Description))
		}
	}
}

// pluginCapabilities returns the capability names an extension declares, by
// registering it on a throwaway runtime.
func pluginCapabilities(ext wago.Extension) []string {
	rt := wago.NewRuntime()
	if err := rt.Use(ext); err != nil {
		return nil
	}
	caps := rt.Capabilities()
	out := make([]string, len(caps))
	for i, c := range caps {
		out[i] = string(c)
	}
	return out
}

// pluginImports builds the merged host imports for a comma-separated plugin list,
// for wiring into the low-level Instantiate path. It fatals on an unknown plugin.
func pluginImports(list string) wago.Imports {
	rt := wago.NewRuntime()
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if err := rt.UsePlugin(name); err != nil {
			fmt.Fprintf(os.Stderr, "%s %v\n", red("wago:"), err)
			os.Exit(1)
		}
	}
	return rt.HostImports()
}

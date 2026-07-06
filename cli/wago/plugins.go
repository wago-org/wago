package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/plugins/log"
	"github.com/wago-org/wago/plugins/metrics"
	"github.com/wago-org/wago/plugins/timer"
	"github.com/wago-org/wago/plugins/wasi"
)

// The built-in plugins compiled into this binary. Each is a small, dependency-
// free package; the Go linker only keeps what is imported, so a leaner binary can
// drop these imports (and heavier third-party plugins live in their own modules,
// wired in via a custom build).
func init() {
	wago.RegisterExtension("timer", func() wago.Extension { return timer.Ext() })
	wago.RegisterExtension("log", func() wago.Extension { return log.Ext() })
	wago.RegisterExtension("metrics", func() wago.Extension { return metrics.Ext() })
	wago.RegisterExtension("wasi", func() wago.Extension {
		return wasi.Ext(wasi.Config{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, Env: os.Environ()})
	})
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
	case "inspect", "show":
		if len(args) < 2 {
			fatal("plugin inspect: need a <name> (see: wago plugin list)")
		}
		pluginInspect(args[1])
	case "add", "install":
		pluginAddCmd(args[1:])
	case "remove", "uninstall", "rm":
		if len(args) < 2 {
			fatal("plugin %s: need a <name>", sub)
		}
		pluginManifestRemove(args[1])
	case "manifest", "declared":
		pluginManifestShow()
	case "build":
		pluginBuild(args[1:])
	default:
		fatal("plugin: unknown subcommand %q (have: list, inspect, add, remove, manifest, build)", sub)
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

// pluginInspect prints one plugin's identity, capabilities, and the host imports
// it provides (with signatures, required capability, and docs).
func pluginInspect(name string) {
	ext, ok := wago.NewExtension(name)
	if !ok {
		fatal("plugin inspect: unknown plugin %q (see: wago plugin list)", name)
	}
	info := ext.Info()
	rt := wago.NewRuntime()
	if err := rt.Use(ext); err != nil {
		fatal("plugin inspect: %v", err)
	}

	fmt.Printf("%s  %s %s  %s\n", bold(name), dim(info.ID), info.Version, dim(string(info.Stability)))
	if info.Description != "" {
		fmt.Printf("  %s\n", info.Description)
	}
	if caps := rt.Capabilities(); len(caps) > 0 {
		strs := make([]string, len(caps))
		for i, c := range caps {
			strs[i] = string(c)
		}
		fmt.Printf("  %s %s\n", dim("capabilities:"), strings.Join(strs, ", "))
	}
	imports := rt.ProvidedImports()
	if len(imports) == 0 {
		return
	}
	fmt.Printf("  %s\n", dim("imports:"))
	for _, s := range imports {
		line := fmt.Sprintf("    %s  %s", cyan(s.Key()), dim(sigString(s.Params, s.Results)))
		if s.HasCapability {
			line += "  " + dim("["+string(s.Capability)+"]")
		}
		fmt.Println(line)
		if s.Docs != "" {
			fmt.Printf("        %s\n", dim(s.Docs))
		}
	}
}

// sigString renders a wasm signature like "(i32, i32) -> i32".
func sigString(params, results []wago.ValType) string {
	ps := make([]string, len(params))
	for i, p := range params {
		ps[i] = p.String()
	}
	sig := "(" + strings.Join(ps, ", ") + ")"
	if len(results) == 0 {
		return sig
	}
	rs := make([]string, len(results))
	for i, r := range results {
		rs[i] = r.String()
	}
	return sig + " -> " + strings.Join(rs, ", ")
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

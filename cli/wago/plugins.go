package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/plugins/log"
	"github.com/wago-org/wago/plugins/metrics"
	"github.com/wago-org/wago/plugins/timer"
	"github.com/wago-org/wago/plugins/wasi/p1"
	"github.com/wago-org/wago/plugins/wasi/unstable"
)

// The built-in plugins compiled into this binary. Each is a small, dependency-
// free package; the Go linker only keeps what is imported, so a leaner binary can
// drop these imports (and heavier third-party plugins live in their own modules,
// wired in via a custom build).
func init() {
	wago.RegisterExtension("timer", func() wago.Extension { return timer.Ext() })
	wago.RegisterExtension("log", func() wago.Extension { return log.Ext() })
	wago.RegisterExtension("metrics", func() wago.Extension { return metrics.Ext() })
	// WASI plugins are selected by path: `wasi` is the default (preview1), and a
	// specific snapshot is `wasi/<version>` (wasi/p1, wasi/unstable). Preview 2
	// (wasi/p2) is a placeholder and not yet implemented.
	wago.RegisterExtension("wasi", func() wago.Extension { return p1.Ext(wasiCLIConfig()) })
	wago.RegisterExtension("wasi/p1", func() wago.Extension { return p1.Ext(wasiCLIConfig()) })
	wago.RegisterExtension("wasi/unstable", func() wago.Extension { return unstable.Ext(wasiCLIConfig()) })
}

// wasiCLIConfig is the base WASI config for the CLI: process stdio and env. argv
// is filled in per run (the run's positional args) by pluginImports.
func wasiCLIConfig() p1.Config {
	return p1.Config{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, Env: os.Environ()}
}

// pluginCmd dispatches `wago plugin <sub>`.
func pluginCmd(args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list", "ls":
		asJSON, _ := hasFlag(args[1:], "--json")
		pluginList(asJSON)
	case "inspect", "show":
		asJSON, rest := hasFlag(args[1:], "--json")
		if len(rest) < 1 {
			fatal("plugin inspect: need a <name> (see: wago plugin list)")
		}
		pluginInspect(rest[0], asJSON)
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

// hasFlag removes flag from args, reporting whether it was present. Used for bare
// boolean flags like --json that extractOpts (value flags) does not handle.
func hasFlag(args []string, flag string) (bool, []string) {
	found := false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == flag {
			found = true
			continue
		}
		rest = append(rest, a)
	}
	return found, rest
}

// pluginList prints (or, with --json, emits) the plugins compiled into this binary:
// id, version, capabilities, and a compatibility hint.
func pluginList(asJSON bool) {
	names := wago.RegisteredPluginNames()
	if asJSON {
		reports := make([]pluginReport, 0, len(names))
		for _, name := range names {
			if ext, ok := wago.NewExtension(name); ok {
				reports = append(reports, buildPluginReport(name, ext))
			}
		}
		printJSON(reports)
		return
	}
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
		if info.Private {
			line += "  " + dim("private")
		}
		if s := compatSummary(info.Compat); s != "" {
			line += "  " + dim(s)
		}
		if len(caps) > 0 {
			line += "  " + dim("caps: "+strings.Join(caps, ", "))
		}
		fmt.Println(line)
		if info.Description != "" {
			fmt.Printf("      %s\n", dim(info.Description))
		}
	}
}

// pluginInspect prints (or, with --json, emits) one plugin's full config: identity,
// provenance, compatibility, capabilities, and the host imports it provides.
func pluginInspect(name string, asJSON bool) {
	ext, ok := wago.NewExtension(name)
	if !ok {
		fatal("plugin inspect: unknown plugin %q (see: wago plugin list)", name)
	}
	report := buildPluginReport(name, ext)
	if asJSON {
		printJSON(report)
		return
	}
	info := report.ExtensionInfo

	header := fmt.Sprintf("%s  %s %s  %s", bold(name), dim(info.ID), info.Version, dim(string(info.Stability)))
	if info.Private {
		header += "  " + dim("· private")
	}
	fmt.Println(header)
	if info.Description != "" {
		fmt.Printf("  %s\n", info.Description)
	}
	kv := func(k, v string) {
		if v != "" {
			fmt.Printf("  %s %s\n", dim(fmt.Sprintf("%-13s", k+":")), v)
		}
	}
	kv("homepage", info.Homepage)
	kv("repository", info.Repository)
	kv("license", info.License)
	kv("authors", strings.Join(info.Authors, ", "))
	kv("tags", strings.Join(info.Tags, ", "))
	kv("compatibility", compatDetail(info.Compat))
	if len(report.Capabilities) > 0 {
		kv("capabilities", strings.Join(report.Capabilities, ", "))
	}
	if len(report.Imports) == 0 {
		return
	}
	fmt.Printf("  %s\n", dim("imports:"))
	for _, s := range report.Imports {
		line := fmt.Sprintf("    %s  %s", cyan(s.Module+"."+s.Name), dim(sigStrings(s.Params, s.Results)))
		if s.Capability != "" {
			line += "  " + dim("["+s.Capability+"]")
		}
		fmt.Println(line)
		if s.Docs != "" {
			fmt.Printf("        %s\n", dim(s.Docs))
		}
	}
}

// pluginReport is the machine-readable (JSON) view of a plugin: its full
// ExtensionInfo plus the capabilities and host imports it contributes.
type pluginReport struct {
	Plugin             string         `json:"plugin"` // the registry name (may be a path, e.g. wasi/p1)
	wago.ExtensionInfo                // flattened: id, name, version, provenance, compatibility, …
	Capabilities       []string       `json:"capabilities,omitempty"`
	Imports            []importReport `json:"imports,omitempty"`
}

// importReport is the JSON view of one provided host import.
type importReport struct {
	Module     string   `json:"module"`
	Name       string   `json:"name"`
	Params     []string `json:"params,omitempty"`
	Results    []string `json:"results,omitempty"`
	Capability string   `json:"capability,omitempty"`
	Docs       string   `json:"docs,omitempty"`
}

// buildPluginReport gathers a plugin's info, capabilities, and imports by
// registering it on a throwaway runtime.
func buildPluginReport(name string, ext wago.Extension) pluginReport {
	rep := pluginReport{Plugin: name, ExtensionInfo: ext.Info()}
	rt := wago.NewRuntime()
	if err := rt.Use(ext); err != nil {
		return rep
	}
	for _, c := range rt.Capabilities() {
		rep.Capabilities = append(rep.Capabilities, string(c))
	}
	for _, s := range rt.ProvidedImports() {
		rep.Imports = append(rep.Imports, importReport{
			Module:     s.Module,
			Name:       s.Name,
			Params:     valTypeStrings(s.Params),
			Results:    valTypeStrings(s.Results),
			Capability: capString(s),
			Docs:       s.Docs,
		})
	}
	return rep
}

func capString(s wago.ImportSpec) string {
	if s.HasCapability {
		return string(s.Capability)
	}
	return ""
}

// compatSummary is a compact compatibility hint for list output: the engine names
// a plugin supports (e.g. "engines: tinygo, wago").
func compatSummary(c wago.Compatibility) string {
	if len(c.Engines) == 0 {
		return ""
	}
	return "engines: " + strings.Join(engineNames(c.Engines), ", ")
}

// compatDetail is the full compatibility line for inspect output, with each
// engine's version constraint and the supported platforms.
func compatDetail(c wago.Compatibility) string {
	var parts []string
	if len(c.Engines) > 0 {
		parts = append(parts, "engines: "+strings.Join(engineTerms(c.Engines), ", "))
	}
	if len(c.Platforms) > 0 {
		parts = append(parts, "platforms: "+strings.Join(c.Platforms, ", "))
	}
	return strings.Join(parts, " · ")
}

// engineNames returns the engine keys, sorted.
func engineNames(engines map[string]string) []string {
	names := make([]string, 0, len(engines))
	for k := range engines {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// engineTerms renders each engine as "name constraint" (or just "name" for an
// unconstrained "*"/"" value), sorted by name.
func engineTerms(engines map[string]string) []string {
	names := engineNames(engines)
	out := make([]string, len(names))
	for i, k := range names {
		if v := engines[k]; v != "" && v != "*" {
			out[i] = k + " " + v
		} else {
			out[i] = k
		}
	}
	return out
}

// valTypeStrings renders wasm value types as their short names ("i32", …).
func valTypeStrings(ts []wago.ValType) []string {
	if len(ts) == 0 {
		return nil
	}
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.String()
	}
	return out
}

// printJSON writes v as indented JSON to stdout, or fatals on a marshal error.
// HTML escaping is off so version constraints render as ">=0.1.0", not ">=…".
func printJSON(v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fatal("json: %v", err)
	}
	fmt.Print(buf.String())
}

// sigStrings renders a wasm signature from pre-stringified types.
func sigStrings(params, results []string) string {
	sig := "(" + strings.Join(params, ", ") + ")"
	if len(results) == 0 {
		return sig
	}
	return sig + " -> " + strings.Join(results, ", ")
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
// for wiring into the low-level Instantiate path. argv is the guest command line
// (the run's positional args); the WASI plugins need it, since the name-registry
// factory cannot supply per-run argv/env. It fatals on an unknown plugin.
func pluginImports(list string, argv []string) wago.Imports {
	out := wago.Imports{}
	if strings.TrimSpace(list) == "" {
		return out
	}
	rt := wago.NewRuntime()
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		cfg := wasiCLIConfig()
		cfg.Args = argv
		switch name {
		case "":
			continue
		case "wasi", "wasi/p1":
			mergeImports(out, p1.Imports(cfg))
		case "wasi/unstable":
			mergeImports(out, unstable.Imports(cfg))
		case "wasi/p2":
			fatal("--plugin wasi/p2: WASI preview 2 (component model) is not implemented yet; use wasi (preview1) or wasi/unstable")
		default:
			if err := rt.UsePlugin(name); err != nil {
				fmt.Fprintf(os.Stderr, "%s %v\n", red("wago:"), err)
				os.Exit(1)
			}
		}
	}
	mergeImports(out, rt.HostImports())
	return out
}

// mergeImports copies src's bindings into dst, overwriting on key collision.
func mergeImports(dst, src wago.Imports) {
	for k, v := range src {
		dst[k] = v
	}
}

package wagocli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

// No plugin is bundled into the default binary. Plugins live in their own modules
// and are compiled into a custom binary from wago.json's dependencies via `wago pkg
// build`; each self-registers through its `register` package. There is no
// per-plugin code or build tag here.

// hasFlag removes flag from args, reporting whether it was present. The Cmd
// framework (cli.go) parses flags now; this is retained only for its unit test.
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
	if len(report.RequiresCapabilities) > 0 {
		kv("requires grants", strings.Join(report.RequiresCapabilities, ", "))
	}
	if len(info.Requires) > 0 {
		kv("requires", strings.Join(info.Requires, ", "))
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

func pluginPlan(asJSON bool) {
	configs, err := projectPlugins(".")
	if err != nil {
		fatal("plugin plan: %v", err)
	}
	plan, err := wago.InspectPluginPlan(configs)
	if err != nil {
		fatal("plugin plan: %v", err)
	}
	if asJSON {
		printJSON(plan)
		return
	}
	if len(plan.Plugins) == 0 {
		fmt.Println(dim("no plugins configured"))
		return
	}
	for i, p := range plan.Plugins {
		line := fmt.Sprintf("  %d. %s  %s", i+1, cyan(p.Name), dim(p.ID))
		if len(p.Capabilities) != 0 {
			caps := make([]string, len(p.Capabilities))
			for i, cap := range p.Capabilities {
				caps[i] = string(cap)
			}
			line += "  " + dim("caps: "+strings.Join(caps, ", "))
		}
		fmt.Println(line)
		if len(p.Provides) != 0 {
			fmt.Printf("      %s %s\n", dim("provides:"), strings.Join(p.Provides, ", "))
		}
		if len(p.Requires) != 0 {
			fmt.Printf("      %s %s\n", dim("requires:"), strings.Join(p.Requires, ", "))
		}
	}
}

func pluginCheck() {
	configs, err := projectPlugins(".")
	if err != nil {
		fatal("plugin check: %v", err)
	}
	plan, err := wago.InspectPluginPlan(configs)
	if err != nil {
		fatal("plugin check: %v", err)
	}
	fmt.Printf("%s %d plugin(s) validated\n", cyan("ok"), len(plan.Plugins))
}

// pluginReport is the machine-readable (JSON) view of a plugin: its full
// ExtensionInfo plus the capabilities and host imports it contributes.
type pluginReport struct {
	Plugin               string         `json:"plugin"` // the registry name (may be a path)
	wago.ExtensionInfo                  // flattened: id, name, version, provenance, compatibility, …
	Capabilities         []string       `json:"capabilities,omitempty"`
	RequiresCapabilities []string       `json:"requiresCapabilities,omitempty"`
	Imports              []importReport `json:"imports,omitempty"`
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
	for _, cap := range ext.Info().RequiresCapabilities {
		rep.RequiresCapabilities = append(rep.RequiresCapabilities, string(cap))
	}
	sort.Strings(rep.RequiresCapabilities)
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

func loadPluginRuntime(cfg *wago.RuntimeConfig, list string) *wago.Runtime {
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	manifest, err := projectPlugins(".")
	if err != nil {
		fatal("plugins: %v", err)
	}
	// Always start with the packages declared in the local wago.json (each with
	// its configured capabilities). --pkg adds any extra packages on top rather
	// than replacing the manifest; names are matched canonically (a leading
	// "github.com/" is optional) and de-duplicated against the manifest.
	selected := append([]wago.PluginConfig(nil), manifest...)
	have := make(map[string]bool, len(manifest))
	for _, item := range manifest {
		have[strings.TrimPrefix(item.Name, "github.com/")] = true
	}
	for _, name := range strings.Split(list, ",") {
		id := strings.TrimPrefix(strings.TrimSpace(name), "github.com/")
		if id == "" || have[id] {
			continue
		}
		have[id] = true
		selected = append(selected, wago.PluginConfig{Name: id})
	}
	if len(selected) != 0 {
		if err := rt.LoadPlugins(selected); err != nil {
			fatal("plugins: %v", err)
		}
	}
	return rt
}

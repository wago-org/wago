package plugin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/pluginmenu"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/tui"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
)

// reviewCapabilities presents a plugin's requestable capabilities in an
// interactive selector — pre-checked, with a trailing "Reject All" row — and
// returns the subset the user grants. `granted` pre-checks the current grants; a
// brand-new plugin starts fully checked. Returns (chosen, ok); ok is false when
// the user rejects (Reject All) or cancels (esc). A plugin that requests nothing
// returns an empty grant with ok=true. On a non-interactive terminal the driver
// keeps the pre-seeded (all/granted) selection, i.e. accept.
//
// Shared by `wago plugin grant` and the install-on-demand flow.
func reviewCapabilities(name string, required, granted []string) (chosen []string, ok bool) {
	if len(required) == 0 {
		return nil, true
	}
	if automation.NoInput() {
		fatal("plugin: --no-input requires --allow, --allow-all, or --deny-all when %s requests capabilities", name)
	}
	grantedSet := map[string]bool{}
	for _, g := range granted {
		grantedSet[g] = true
	}
	items := make([]tui.SelectItem, len(required))
	for i, c := range required {
		items[i] = tui.SelectItem{Label: c, Description: CapabilityDescription(c), On: len(granted) == 0 || grantedSet[c]}
	}
	m := &tui.MultiSelect{
		Title:  fmt.Sprintf("Package %s wants to use the following capabilities:", name),
		Prompt: "↑/↓ move · space toggle · enter/→ accept · r reject all · esc cancel",
		Items:  items,
	}
	// Enter/right accepts the selected items; r clears and submits (grant
	// nothing) — both return ok=true. Only esc cancels, leaving grants as-is.
	_, cancelled := tui.Run(m)
	if cancelled {
		return nil, false
	}
	return m.Chosen(), true
}

func chooseCapabilities(name string, required, granted, requested []string, grantAll, denyAll bool) ([]string, bool) {
	switch {
	case grantAll:
		return append([]string(nil), required...), true
	case denyAll:
		return []string{}, true
	case requested != nil:
		allowed := make(map[string]bool, len(required))
		for _, capability := range required {
			allowed[capability] = true
		}
		for _, capability := range requested {
			if !allowed[capability] {
				fatal("plugin: %s does not request %q", name, capability)
			}
		}
		return append([]string(nil), requested...), true
	default:
		return reviewCapabilities(name, required, granted)
	}
}

// pkgGrant interactively edits which of a compiled-in plugin's requestable
// capabilities are granted in the active wago-lock.json (local or global per scope).
func pkgGrant(name string, useGlobal bool, requested []string, grantAll, denyAll bool) {
	id := strings.TrimPrefix(strings.TrimSpace(name), "github.com/")
	src, err := depsSource(useGlobal)
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	deps, err := project.Dependencies(src)
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	if id == "" {
		selected, ok := chooseGrantPlugin(src, deps)
		if !ok {
			fmt.Println("Cancelled.")
			return
		}
		id = selected
	}
	if !ContainsDependency(deps, id) {
		fatal("plugin grant: %q is not installed — run `wago add %s` first", name, name)
	}
	// The base binary doesn't have the package compiled in, so build (or reuse)
	// the custom binary and inspect *it* for the package's requestable
	// capabilities — the same way the install trigger does.
	buildDir, err := buildDirFor(useGlobal)
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	changed, err := syncLockedPluginVersions(buildDir, src, false)
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	bin, _, err := pluginbuild.EnsureBinary(buildDir, deps, changed, false, pluginBuildConfig())
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	required, err := inspectRequiredCapabilities(bin, id)
	if err != nil {
		fatal("plugin grant: inspecting %s: %v", id, err)
	}
	chosen, ok := chooseCapabilities(id, required, project.Grants(src, id), requested, grantAll, denyAll)
	if !ok {
		fmt.Println(dim("no changes"))
		return
	}
	// Keep the lockfile snapshot in sync so a later install doesn't re-prompt.
	lock, err := project.ReadLock(src)
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	entry := lock.Packages[id]
	entry.RequiredCapabilities = required
	entry.Capabilities, err = json.Marshal(chosen)
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	lock.Packages[id] = entry
	if err := project.WriteLock(src, lock); err != nil {
		fatal("plugin grant: %v", err)
	}
	if len(chosen) == 0 {
		fmt.Printf("%s %s now has no capability grants\n", cyan("✓"), id)
		return
	}
	fmt.Printf("%s granted %s: %s\n", cyan("✓"), id, strings.Join(chosen, ", "))
}

func chooseGrantPlugin(src string, dependencies []string) (string, bool) {
	if len(dependencies) == 0 {
		fatal("plugin grant: no plugins enabled")
	}
	lock, err := project.ReadLock(src)
	if err != nil {
		fatal("plugin grant: %v", err)
	}
	packages := make([]pluginmenu.Package, 0, len(dependencies))
	for _, dependency := range dependencies {
		id := strings.TrimPrefix(dependency, "github.com/")
		version := strings.TrimSpace(lock.Packages[id].Version)
		if version == "" {
			version = "unresolved"
		} else {
			version = DisplayVersion(version)
		}
		packages = append(packages, pluginmenu.Package{
			Name: id, Version: version,
		})
	}
	return pluginmenu.Select("Select installed plugin", packages)
}

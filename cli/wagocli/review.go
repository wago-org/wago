package wagocli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

// capabilityDocs describes the standard plugin capabilities for the review UI.
// Unknown capabilities render with no description.
var capabilityDocs = map[string]string{
	"host.imports":       "provide host-import functions to guests",
	"host.environment":   "read the host environment (args, env, clock, fs)",
	"module.compile":     "hook module compilation",
	"instance.lifecycle": "hook instance create/destroy",
	"instance.invoke":    "hook guest invocations",
	"runtime.lifecycle":  "hook runtime start/stop",
	"instance.manage":    "create and manage guest instances",
}

func capabilityDoc(cap string) string { return capabilityDocs[cap] }

// reviewCapabilities presents a plugin's requestable capabilities in an
// interactive selector — pre-checked, with a trailing "Reject All" row — and
// returns the subset the user grants. `granted` pre-checks the current grants; a
// brand-new plugin starts fully checked. Returns (chosen, ok); ok is false when
// the user rejects (Reject All) or cancels (esc). A plugin that requests nothing
// returns an empty grant with ok=true. On a non-interactive terminal the driver
// keeps the pre-seeded (all/granted) selection, i.e. accept.
//
// Shared by `wago pkg grant` and the install-on-demand flow.
func reviewCapabilities(name string, required, granted []string) (chosen []string, ok bool) {
	if len(required) == 0 {
		return nil, true
	}
	grantedSet := map[string]bool{}
	for _, g := range granted {
		grantedSet[g] = true
	}
	items := make([]selItem, len(required))
	for i, c := range required {
		items[i] = selItem{label: c, desc: capabilityDoc(c), on: len(granted) == 0 || grantedSet[c]}
	}
	m := &multiSelect{
		title:  fmt.Sprintf("Package %s wants to use the following capabilities:", name),
		prompt: "Use arrow keys and space to select and toggle on and off. Enter to submit.",
		items:  items,
		reject: true,
	}
	cancelled := runSelector(m)
	if cancelled || m.rejected {
		return nil, false
	}
	return m.chosen(), true
}

// pkgGrant interactively edits which of a compiled-in plugin's requestable
// capabilities are granted in the active wago.json (local or global per scope).
func pkgGrant(name string, useGlobal bool) {
	id := strings.TrimPrefix(strings.TrimSpace(name), "github.com/")
	ext, ok := wago.NewExtension(id)
	if !ok {
		fatal("pkg grant: %q is not compiled into this binary — add it with `wago pkg add` first", name)
	}
	required := make([]string, 0, len(ext.Info().RequiresCapabilities))
	for _, c := range ext.Info().RequiresCapabilities {
		required = append(required, string(c))
	}
	sort.Strings(required)

	src, err := depsSource(useGlobal)
	if err != nil {
		fatal("pkg grant: %v", err)
	}
	chosen, ok := reviewCapabilities(id, required, pluginGrants(src, id))
	if !ok {
		fmt.Println(dim("no changes"))
		return
	}
	if err := setPluginGrants(src, id, chosen); err != nil {
		fatal("pkg grant: %v", err)
	}
	if len(chosen) == 0 {
		fmt.Printf("%s %s now has no capability grants\n", cyan("✓"), id)
		return
	}
	fmt.Printf("%s granted %s: %s\n", cyan("✓"), id, strings.Join(chosen, ", "))
}

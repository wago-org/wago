package wagocli

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

// reviewCapabilities presents a plugin's requestable capabilities and returns the
// subset the user grants. `required` is the plugin's full requestable set;
// `granted` is the currently-granted subset (shown pre-checked). It prompts
// Accept-all / Modify / Reject; Modify opens the interactive selector. Returns
// (chosen, ok); ok is false when the user rejects or cancels. A plugin that
// requests nothing returns an empty grant with ok=true.
//
// This is shared by `wago pkg grant` and (next) the install-on-demand flow.
func reviewCapabilities(name string, required, granted []string) (chosen []string, ok bool) {
	if len(required) == 0 {
		fmt.Printf("%s requests no capabilities.\n", cyan(name))
		return nil, true
	}
	grantedSet := map[string]bool{}
	for _, g := range granted {
		grantedSet[g] = true
	}
	fmt.Printf("%s requests these capabilities:\n", cyan(name))
	for _, c := range required {
		mark := dim("◦")
		if grantedSet[c] {
			mark = cyan("✔")
		}
		fmt.Printf("  %s %s\n", mark, c)
	}
	fmt.Printf("\n%s ", bold("[A]ccept all  [M]odify  [R]eject:"))

	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "a", "accept", "y", "yes", "":
		return append([]string(nil), required...), true
	case "m", "modify", "e", "edit":
		items := make([]selItem, len(required))
		for i, c := range required {
			// Pre-check current grants; a brand-new plugin (no grants yet) starts
			// fully selected so "accept all" and "modify" agree by default.
			items[i] = selItem{label: c, on: len(granted) == 0 || grantedSet[c]}
		}
		m := &multiSelect{title: "grant capabilities for " + name, items: items}
		if cancelled := runSelector(m); cancelled {
			return nil, false
		}
		return m.chosen(), true
	default: // r, reject, n, no, anything else
		return nil, false
	}
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

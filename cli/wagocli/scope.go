package wagocli

import (
	"errors"
	"os"
)

// scopeGlobal is the pure decision behind the `wago add`/`wago plugin` commands' plugin-set
// scope: given the explicit --global/--local flags and whether the current
// directory has a wago.json, report whether to operate on the global set (or an
// error for conflicting flags). With no flags it mirrors what `wago run` already
// does (activePluginSet): a wago.json in the cwd selects the local project set,
// and its absence falls back to the CLI-wide global set.
func scopeGlobal(explicitGlobal, explicitLocal, cwdHasManifest bool) (bool, error) {
	switch {
	case explicitGlobal && explicitLocal:
		return false, errors.New("choose either --global or --local, not both")
	case explicitGlobal:
		return true, nil
	case explicitLocal:
		return false, nil
	case cwdHasManifest:
		return false, nil // a project manifest is present — use it
	default:
		return true, nil // no local manifest — the global set is the default
	}
}

// resolveScope applies scopeGlobal against the real filesystem, fataling on
// conflicting flags. It returns whether to operate on the global plugin set.
func resolveScope(explicitGlobal, explicitLocal bool) bool {
	_, statErr := os.Stat(projectManifestPath("."))
	useGlobal, err := scopeGlobal(explicitGlobal, explicitLocal, statErr == nil)
	if err != nil {
		fatal("pkg: %v", err)
	}
	return useGlobal
}

package handoff

import (
	"path/filepath"
	"strings"
)

// RuntimeOwnsPluginCommand reports whether a plugin subcommand inspects the
// selected runtime rather than mutating manager-owned plugin state.
func RuntimeOwnsPluginCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "list", "ls", "inspect":
		return true
	default:
		return false
	}
}

// LooksLikeRuntimeTarget reports whether value is plausibly a module path.
func LooksLikeRuntimeTarget(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasSuffix(lower, ".wasm") ||
		strings.HasSuffix(lower, ".wat") ||
		strings.HasSuffix(lower, ".wago") ||
		strings.ContainsAny(value, `/\`) ||
		filepath.Ext(value) != ""
}

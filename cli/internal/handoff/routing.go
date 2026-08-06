package handoff

import (
	"os"
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

// LooksLikeRuntimeTarget reports whether value should enter the runtime's
// implicit-run path. Manager and runtime both use this decision so a handoff
// can never turn a manager-recognized target into a runtime-unknown command.
func LooksLikeRuntimeTarget(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasSuffix(lower, ".wasm") ||
		strings.HasSuffix(lower, ".wago") ||
		existingFile(value)
}

func existingFile(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	return err == nil && !info.IsDir()
}

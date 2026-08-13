package wasmcall

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago"
)

// ResolveExport applies the command entry-point convention shared by run and
// standalone executables: an explicit request, _start, main, or the sole
// exported function, in that order.
func ResolveExport(compiled *wago.Compiled, requested string) (string, error) {
	names := compiled.ExportedFunctions()
	if requested != "" {
		if _, ok := compiled.Exports[requested]; !ok {
			return "", fmt.Errorf("no exported function %q (have: %s)", requested, strings.Join(names, ", "))
		}
		return requested, nil
	}
	for _, candidate := range []string{"_start", "main"} {
		if _, ok := compiled.Exports[candidate]; ok {
			return candidate, nil
		}
	}
	if len(names) == 1 {
		return names[0], nil
	}
	if len(names) == 0 {
		return "", fmt.Errorf("module exports no functions")
	}
	return "", fmt.Errorf("multiple exports; select one (have: %s)", strings.Join(names, ", "))
}

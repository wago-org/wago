package wago

import (
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Keep panic-value construction and formatting shared across GC validation
// failures. Callers still panic with the original private values, so dispatcher
// recovery and trap codes retain their existing semantics.
//
//go:noinline
func gcHelperFailure(err error) any {
	return gcStructHelperError{err: err}
}

//go:noinline
func gcHelperFailuref(format string, args ...any) any {
	return gcStructHelperError{err: fmt.Errorf(format, args...)}
}

//go:noinline
func gcHelperTrap(code coreruntime.TrapCode) any {
	return gcStructHelperTrap{code: code}
}

//go:build !tinygo

package railshot

import "runtime"

func compilerPanicStack() []byte {
	// A fixed bound keeps malformed-input diagnostics from growing without limit.
	stack := make([]byte, 16<<10)
	return stack[:runtime.Stack(stack, false)]
}

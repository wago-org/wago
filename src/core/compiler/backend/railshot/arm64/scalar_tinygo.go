//go:build arm64 && tinygo

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The pilot is measured with the standard Go toolchain. TinyGo retains the
// established function path so its lean release does not pay for a second
// unqualified compiler implementation. Admission still precedes emission.
var sharedScalarEnabled = false

func (f *fn) admitScalar(*wasm.Func) shared.ScalarSummary { return shared.ScalarSummary{} }
func (f *fn) scalarBody(*wasm.Func) error {
	return fmt.Errorf("arm64: shared scalar pilot is unavailable in TinyGo builds")
}

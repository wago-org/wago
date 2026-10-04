//go:build arm64 && (tinygo || wago_profile)

package arm64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The measured pilot uses standard Go production builds. TinyGo retains the
// established path for release size; profiling builds retain it for code
// identity whether source/unwind recording is enabled or disabled. Admission
// still precedes emission.
var sharedScalarEnabled = false

func (f *fn) admitScalar(*wasm.Func) shared.ScalarSummary { return shared.ScalarSummary{} }
func (f *fn) scalarBody(*wasm.Func) error {
	return fmt.Errorf("arm64: shared scalar pilot is unavailable in TinyGo or profiling builds")
}

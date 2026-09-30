//go:build wagodebug

package wago

import (
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

const nativeGCEntryValidationEnabled = true

// validateNativeGCEntry retains a coarse hardened assertion at the untrusted
// Go-to-native boundary without charging every production GC access. It checks
// only immutable ABI/view facts; mutable collector backing remains dynamically
// validated by generated semantic access code.
func validateNativeGCEntry(in *Instance) error {
	if in == nil || in.gcNativeView == nil {
		return nil
	}
	if in.jm == nil || in.jm.GCNativeViewPtr() != uintptr(unsafe.Pointer(in.gcNativeView)) {
		return fmt.Errorf("wagodebug: native GC basedata view pointer mismatch")
	}
	if err := gc.ValidateNativeInstanceView(in.gcNativeView, in.gc, uint32(len(in.c.Types))); err != nil {
		return fmt.Errorf("wagodebug: native GC entry: %w", err)
	}
	return nil
}

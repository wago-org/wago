//go:build !(wago_gc_compact_reads || (wago_gc_compact_alloc || (wago_gc_compact_refstore || (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || wago_gc_compact_copy))))) || wago_gc_compact_refstruct)

package wago

import (
	"errors"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const compactNativeRuntimeEnabled = false

type compactRuntimeState struct{}

func buildCompactRuntimeState(*gc.Collector, []gc.TypeID) (compactRuntimeState, error) {
	return compactRuntimeState{}, errors.New("wago: compact native runtime build required")
}
func (compactRuntimeState) pointer() uintptr         { return 0 }
func (compactRuntimeState) validate(*Instance) error { return nil }

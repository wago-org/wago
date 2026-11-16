//go:build (wago_gc_compact_reads || (wago_gc_compact_alloc || (wago_gc_compact_refstore || (wago_gc_compact_scalar_arrays || wago_gc_compact_array_alloc))))

package wago

import (
	"errors"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"unsafe"
)

const compactNativeRuntimeEnabled = true

// The typed pointer keeps the immutable canonical map and descriptor alive.
// Only this opt-in build adds one pointer to Instance.
type compactRuntimeState struct{ view *gc.CompactInstanceView }

func buildCompactRuntimeState(c *gc.Collector, ids []gc.TypeID) (compactRuntimeState, error) {
	v, e := gc.BuildCompactInstanceView(c, ids)
	if e == nil {
		e = gc.ValidateCompactInstanceView(v, c)
	}
	return compactRuntimeState{v}, e
}
func (s compactRuntimeState) pointer() uintptr { return uintptr(unsafe.Pointer(s.view)) }
func (s compactRuntimeState) validate(in *Instance) error {
	if s.view == nil {
		return nil
	}
	if in.gcNativeView != nil || in.gcDirectView != nil || in.jm == nil || in.jm.GCNativeViewPtr() != s.pointer() {
		return errors.New("wago: compact native view mismatch")
	}
	return gc.ValidateCompactInstanceView(s.view, in.gc)
}

//go:build !(wago_gc_compact_reads || (wago_gc_compact_alloc || wago_gc_compact_refstore))

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

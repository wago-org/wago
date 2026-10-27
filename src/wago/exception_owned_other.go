//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import (
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (*Compiled) needsOwnedExceptionReferences() bool                  { return false }
func (*Compiled) setOwnedExceptionReferences(bool)                     {}
func newNativeExceptionTag(_ *Compiled, _ int, fallback uint64) uint64 { return fallback }
func releaseNativeExceptionTags(*Compiled, []byte)                     {}
func releaseInstanceExceptionTags(*Instance)                           {}
func drainExceptionReclaims(*gc.Collector)                             {}

func dispatchOwnedExceptionHelper(*Instance, uintptr, uint32, uint32, []uint64, []uint64, *coreruntime.Engine) bool {
	return false
}

func ownedExceptionReferencesRuntime() bool { return false }
func ownedExceptionExactValueType(*gc.Collector, gc.Ref, gc.TypeID) (ValueTypeDescriptor, bool) {
	return ValueTypeDescriptor{}, false
}
func ownedExceptionReferenceMatches(*gc.Collector, gc.Ref, ValueTypeDescriptor) (bool, bool) {
	return false, false
}

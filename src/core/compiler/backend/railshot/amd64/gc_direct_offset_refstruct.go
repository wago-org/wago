//go:build amd64

package amd64

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
)

// Attested Wasm typing and checked ingress establish that a loaded collector
// reference is valid or nullable. It remains an exact root across future calls.
func (f *fn) emitTrustedDirectStructRefGet(t, fieldIndex uint32) bool {
	if !gcTrustedDirectRefStructs || t > math.MaxInt32/4 {
		return false
	}
	field, ok := f.stagedStructField(t, fieldIndex)
	if !ok || !nativeGCCollectorRefStorage(f.m, t, field.Storage()) {
		return false
	}
	off, size, final, ok := nativeGCStructFieldLayout(f.m, t, fieldIndex)
	if !ok || !final || size != 4 || uint64(off)+uint64(gc.PayloadOffset)+4 > math.MaxInt32 {
		return false
	}
	f.flush()
	floor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	obj, done := f.emitTrustedDirectOffset(f.popValue())
	r := f.allocReg(maskOf(obj))
	f.a.Load32(r, obj, int32(gc.PayloadOffset+off))
	done()
	f.spillFloor = floor
	f.pushReg(r, mtI64)
	f.markTopGCReference()
	return true
}

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_gc || !wago_no_exception_handling)

package wago

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type compactExternNativeTestRoots struct{ gcNativeFrameRoots }

func (r *compactExternNativeTestRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	r.rangeChain(fn, nil, nil)
}

func (r *compactExternNativeTestRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	return r.rangeChain(nil, sink, nil)
}

func TestRV64CompactExternNativeFrameRoots(t *testing.T) {
	in := managedCollectorTestInstance(t, gc.ProfileThroughput)
	state, err := in.compactExternCollector(nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := in.gc.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	word, err := state.externFromAny(leaf, gc.EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 16)
	var pin runtime.Pinner
	pin.Pin(&frame[0])
	defer pin.Unpin()
	binary.LittleEndian.PutUint64(frame[8:], word)
	roots := compactExternNativeTestRoots{gcNativeFrameRoots{owner: in, base: uintptr(unsafe.Pointer(&frame[0])), offsets: []uint32{shared.GCFrameExternRefRootFlag | 8}, frameBytes: 16, frameLayout: gcNativeFrameLayoutRISCV64, codeBase: 1, codeBytes: 1, allowExternalReturn: true}}
	if err := in.gc.CollectFull(&roots); err != nil {
		t.Fatal(err)
	}
	if _, err := in.gc.ObjectType(leaf); err != nil || binary.LittleEndian.Uint64(frame[8:]) != word {
		t.Fatalf("extern frame lost its value or rewrote opaque word: %v", err)
	}
	binary.LittleEndian.PutUint64(frame[8:], 0)
	if err := in.gc.CollectFull(&roots); err != nil {
		t.Fatal(err)
	}
	if len(state.shells) != 0 {
		t.Fatal("cleared extern frame retained its shell")
	}
}

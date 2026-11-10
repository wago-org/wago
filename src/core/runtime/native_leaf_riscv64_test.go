//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"github.com/wago-org/wago/internal/runtimebridge"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"reflect"
	goruntime "runtime"
	"testing"
)

//go:noinline
func nativeLeafRV64Inc(v int32) int32 { return v + 1 }

//go:noinline
func nativeLeafRV64Add(a, b int32) int32 { return a + b }
func TestNativeLeafRV64RejectsUnsafeWords(t *testing.T) {
	words := []uint32{0x00013503, 0x0000006f, 0x000000ef, 0x00000597, 0x00110113, 0x001c8c93}
	for _, w := range words {
		defined := uint32(1<<10 | 1<<11)
		if scalarLeafInstructionRV64(w, &defined) {
			t.Fatalf("unsafe instruction %#x admitted", w)
		}
	}
	var a enc.Asm
	a.Addi(enc.X10, enc.X7, 1)
	defined := uint32(1 << 10)
	if scalarLeafInstructionRV64(binary.LittleEndian.Uint32(a.B), &defined) {
		t.Fatal("undefined scratch read")
	}
	if scalarLeafBodyRV64(func(v int64) int64 { return v + 1 }, 1) != nil {
		t.Fatal("wrong Go ABI admitted")
	}
	if scalarLeafBodyRV64(nativeLeafRV64Inc, 1) == nil || scalarLeafBodyRV64(nativeLeafRV64Add, 2) == nil {
		for _, fn := range []any{nativeLeafRV64Inc, nativeLeafRV64Add} {
			pc := reflect.ValueOf(fn).Pointer()
			words := make([]uint32, 8)
			for i := range words {
				words[i] = *(*uint32)(offHeapPointer(pc + uintptr(i*4)))
			}
			t.Logf("Go=%s function=%s words=%08x", goruntime.Version(), goruntime.FuncForPC(pc).Name(), words)
		}
		t.Fatal("pure Go i32 body rejected")
	}
}
func TestNativeLeafRV64ExecutesAndKeepsBoundedCallbacks(t *testing.T) {
	e, j, args, results, trap, ctrl := hostFixtureRV64(t, 64)
	access := runtimebridge.GrantHostScalarCall()
	admitted, err := RegisterNativeScalarLeaf(access, ctrl, nativeLeafRV64Inc, 1)
	if err != nil || !admitted {
		t.Fatal(admitted, err)
	}
	t.Cleanup(func() { unregisterNativeScalarLeaf(ctrl) })
	code := hostRoundtripImportRV64(t, 1, 1, 1, false, 0)
	binary.LittleEndian.PutUint64(args, 0xffffffff)
	calls := 0
	host := func(_ uintptr, _ uint32, in, out []uint64) { calls++; out[0] = in[0] + 7 }
	if err := e.CallWithHostBase(code, args, j.LinMemBase(), trap, results, ctrl, host); err != nil || binary.LittleEndian.Uint64(results) != 0 || calls != 0 {
		t.Fatal("native leaf", calls, err)
	}
	if err := e.CallWithHostBaseScalarBounded(access, code, args, j.LinMemBase(), trap, results, ctrl, 0x10001, func(a, b uint64) uint64 { calls++; return 21 }); err != nil || calls != 1 || binary.LittleEndian.Uint64(results) != 21 {
		t.Fatal("bounded callback bypassed", calls, err)
	}
	foreign := hostRoundtripImportRV64(t, 1, 1, 1, false, 1)
	if err := e.CallWithHostBase(foreign, args, j.LinMemBase(), trap, results, ctrl, host); err != nil || calls != 2 {
		t.Fatal("foreign import did not park", calls, err)
	}
	if _, err := RegisterNativeScalarLeaf(access, ctrl, nativeLeafRV64Inc, 1); err == nil {
		t.Fatal("duplicate native owner")
	}
	if alloc := testing.AllocsPerRun(32, func() {
		if err := e.CallWithHostBase(code, args, j.LinMemBase(), trap, results, ctrl, host); err != nil {
			panic(err)
		}
	}); alloc != 0 {
		t.Fatal("native leaf warm allocations", alloc)
	}
	dispatch := make([]byte, ImportDispatchEntryBytes)
	if !BindNativeScalarLeafImport(access, ctrl, dispatch) || binary.LittleEndian.Uint64(dispatch[ImportDispatchCodePtrOffset:]) == 0 {
		t.Fatal("direct native leaf import not bound")
	}
	unregisterNativeScalarLeaf(ctrl)
	if nativeScalarLeafPtr(ctrl) != 0 {
		t.Fatal("native leaf owner leaked")
	}
}

// A table activation may borrow another instance's trampoline. An identical
// import ordinal and slot shape do not authorize that instance's leaf body.
func TestNativeLeafRV64ForeignControlUsesHostDispatch(t *testing.T) {
	_, _, _, _, _, owner := hostFixtureRV64(t, 64)
	admitted, err := RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), owner, nativeLeafRV64Inc, 1)
	if err != nil || !admitted {
		t.Fatal(admitted, err)
	}
	defer unregisterNativeScalarLeaf(owner)
	leaf, ok := nativeLeafHosts.Load(&owner[0])
	if !ok {
		t.Fatal("missing native leaf owner")
	}
	t.Logf("Go body=%dB trampoline=%dB executable mapping=%dB", len(scalarLeafBodyRV64(nativeLeafRV64Inc, 1)), leaf.direct-slicePtr(leaf.code), len(leaf.code))
	e, j, args, results, trap, foreign := hostFixtureRV64(t, 64)
	if err := j.BindTrapCell(trap); err != nil {
		t.Fatal(err)
	}
	if err := InitHostCtrlFrame(foreign); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(foreign[hcTrampoline:], uint64(nativeScalarLeafPtr(owner)))
	binary.LittleEndian.PutUint64(args, 5)
	code := hostRoundtripImportRV64(t, 1, 1, 1, false, 0)
	calls := 0
	callback := func(ctrl uintptr, index uint32, in, out []uint64) {
		if ctrl != slicePtr(foreign) || index != 0 {
			t.Fatal("wrong foreign control or import", index)
		}
		calls++
		out[0] = in[0] + 7
	}
	var input, output [maxHostArity]uint64
	if err := e.callWithHostLoop(code, args, j.LinMemBase(), trap, results, foreign, slicePtr(foreign), callback, nil, input[:], output[:]); err != nil || calls != 1 || binary.LittleEndian.Uint64(results) != 12 {
		t.Fatal("borrowed trampoline selected wrong host", calls, binary.LittleEndian.Uint64(results), err)
	}
}

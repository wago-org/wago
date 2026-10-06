//go:build amd64 && (linux || darwin || windows)

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
	"unsafe"
)

func BenchmarkCompileSpillPressure(b *testing.B) {
	for _, n := range []int{2048, 8192} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			body := []byte{0}
			for i := 0; i < n; i++ {
				body = append(body, 0x20, 0, 0x9a)
			}
			for i := 0; i < n; i++ {
				body = append(body, 0x1a)
			}
			body = append(body, 0x0b)
			m := benchDecodeValidateModule(b, benchModuleBytes([]benchFuncDef{{params: []wasm.ValType{wasm.F64}, body: body}}, false))
			benchmarkCompileModule(b, m)
		})
	}
}

func TestLiveSpillExtentReclaimsSlots(t *testing.T) {
	if unsafe.Sizeof(stack{}) != 88 {
		t.Fatal("unexpected scratch size")
	}
	f := fn{s: newStackWithCap(16)}
	a := f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 2})
	v := f.pushValue(storage{kind: stSlot, typ: mtV128, slot: 8})
	if got := f.curSpillSlot(); got != 10 {
		t.Fatalf("vector extent=%d", got)
	}
	_ = f.s.firstUnspilled()
	f.erase(v)
	if got := f.curSpillSlot(); got != 3 {
		t.Fatalf("extent after erase=%d", got)
	}
	f.spillFloor = 20
	if got := f.curSpillSlot(); got != 20 {
		t.Fatalf("reserved floor=%d", got)
	}
	f.spillFloor = 0
	if got := f.curSpillSlot(); got != 3 {
		t.Fatalf("floor leaked into extent=%d", got)
	}
	f.replaceStorage(a, storage{kind: stReg, typ: mtI64, reg: R8})
	if f.s.firstUnspilled() != a {
		t.Fatal("slot-to-register transition was skipped")
	}
	if got := f.curSpillSlot(); got != 0 {
		t.Fatalf("removed slot extent=%d", got)
	}
	f.replaceStorage(a, storage{kind: stSlot, typ: mtI64, slot: 4})
	if got := f.curSpillSlot(); got != 5 {
		t.Fatalf("replacement extent=%d", got)
	}
	f.s.reset()
	if got := f.curSpillSlot(); got != 0 {
		t.Fatalf("reused stack extent=%d", got)
	}
}

func TestLiveSpillCacheStackRebuild(t *testing.T) {
	f := fn{s: newStackWithCap(16)}
	f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 9})
	old := f.pushReg(R8, mtI64)
	if f.s.firstUnspilled() != old {
		t.Fatal("missing original victim")
	}
	f.setDepthTypes([]machineType{mtI64})
	if got := f.curSpillSlot(); got != 1 {
		t.Fatalf("rebuilt extent=%d, want 1", got)
	}
	current := f.pushReg(R8, mtI64)
	if got := f.s.firstUnspilled(); got != current {
		t.Fatal("victim search retained a detached prefix")
	}
	f.setDepthTypes(nil)
	if got := f.curSpillSlot(); got != 0 {
		t.Fatalf("empty rebuilt extent=%d", got)
	}
	if f.s.firstUnspilled() != f.s.head {
		t.Fatal("empty rebuild retained a victim")
	}
}

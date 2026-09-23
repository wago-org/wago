//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

func overlappingSlotCallCode(indirect bool) []byte {
	a := &a64.Asm{}
	var stats CodegenStats
	f := fn{a: a, s: newStack(), m: &wasm.Module{}, stats: &stats, maxSpill: 1}
	a.StpPre(FP, LR, SP, -16)
	a.SubImm64(SP, SP, 64)
	a.MovImm64(X16, 42)
	f.st64(SP, f.spillOff(0), X16)
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: 99})
	f.pushValue(storage{kind: stSlot, typ: mtI64, slot: 0})

	target, callee, indirectReg := -1, -1, regNone
	if indirect {
		target = a.Adr(X2)
		indirectReg = X2
		f.pinned = f.pinned.add(X2)
	} else {
		callee = 0
	}
	ret := f.emitRegisterCallVia(&wasm.CompType{Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I64}}, -1, true, callee, indirectReg)
	a.AddImm64(SP, SP, 64)
	a.LdpPost(FP, LR, SP, 16)
	a.Ret()
	callee = a.Len()
	a.Ret()
	if indirect {
		if !a.PatchAdr(target, callee) {
			panic("indirect target out of range")
		}
	} else if !a.PatchBranch26(int(ret)-4, callee) {
		panic("direct target out of range")
	}
	return a.B
}

func TestRegisterCallOverlappingArgumentSlotARM64(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		name := "direct"
		if indirect {
			name = "indirect"
		}
		t.Run(name, func(t *testing.T) {
			code, err := arm64spike.MapExec(overlappingSlotCallCode(indirect))
			if err != nil {
				t.Fatal(err)
			}
			defer coreruntime.Unmap(code)
			if got := arm64spike.Call2(uintptr(unsafe.Pointer(&code[0])), 0, 0); got != 42 {
				t.Fatalf("call argument = %d, want 42", got)
			}
		})
	}
}

func BenchmarkRegisterCallOverlappingArgumentSlotARM64(b *testing.B) {
	for _, indirect := range []bool{false, true} {
		name := "direct"
		if indirect {
			name = "indirect"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				overlappingSlotCallCode(indirect)
			}
		})
	}
}

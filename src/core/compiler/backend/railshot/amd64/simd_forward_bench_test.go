//go:build amd64 && (linux || darwin || windows)

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

// Lazy vector reads followed by drops should not repeatedly search older operands.
func BenchmarkCompileVectorLocalReads(b *testing.B) {
	for _, n := range []int{512, 8192} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			body := []byte{0}
			for i := 0; i < n; i++ {
				body = append(body, 0x20, 0)
			}
			for i := 0; i < n; i++ {
				body = append(body, 0x1a)
			}
			body = append(body, 0x0b)
			m := benchDecodeValidateModule(b, benchModuleBytes([]benchFuncDef{{params: []wasm.ValType{wasm.V128}, body: body}}, false))
			benchmarkCompileModule(b, m)
		})
	}
}

func TestVectorForwardIgnoresDetachedOwner(t *testing.T) {
	f := fn{s: newStackWithCap(8)}
	old := f.pushValue(storage{kind: stReg, typ: mtV128, reg: 3, cval: 1})
	f.fregUser[3] = old
	f.erase(old)
	if f.forwardV128Local(0, true) {
		t.Fatal("forwarded a detached register owner")
	}
	live := f.pushValue(storage{kind: stReg, typ: mtV128, reg: 4, cval: 1})
	f.fregUser[4] = live
	if !f.forwardV128Local(0, true) {
		t.Fatal("missed live alias")
	}
	got := f.s.back().st
	if got.kind != stLocalReg || got.reg != 4 || got.idx != 0 {
		t.Fatalf("wrong forwarded value: %#v", got)
	}
}

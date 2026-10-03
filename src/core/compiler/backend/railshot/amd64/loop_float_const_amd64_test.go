//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCallFreeLoopFloatConstantScopeAMD64(t *testing.T) {
	appendF64 := func(body []byte, value float64) []byte {
		body = append(body, 0x44)
		return binary.LittleEndian.AppendUint64(body, math.Float64bits(value))
	}
	body := []byte{
		0x01, 0x01, 0x7c, // one f64 local (accumulator)
		0x02, 0x40, 0x03, 0x40, // block; loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // if count == 0, exit block
		0x20, 0x01, // accumulator
	}
	body = appendF64(body, 1.5)
	body = append(body,
		0xa0, 0x21, 0x01, // f64.add; local.set accumulator
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // count--
		0x0c, 0x00, 0x0b, 0x0b, // backedge; end loop; end block
		0x10, 0x01, 0x1a, // call a clobbering helper; drop result
		0x20, 0x01, // accumulator
	)
	body = appendF64(body, 1.5)           // use again after the loop and call
	body = append(body, 0xa0, 0xbd, 0x0b) // add; reinterpret as i64; end
	helper := []byte{0x00}
	helper = appendF64(helper, 7)
	helper = append(helper, 0x1a, 0x41, 0x00, 0x0b)
	m := modFuncs(t,
		funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, body},
		funcDef{nil, []wasm.ValType{wasm.I32}, helper},
	)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"inline": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if diagnosticsEnabled {
		if got := stats.Funcs[0].Peephole["callfree-loop-fconst"]; got == 0 {
			t.Fatalf("call-free loop constant cache not used: %v", stats.Funcs[0].Peephole)
		}
	}
	for _, n := range []uint64{0, 1, 2, 17} {
		want := math.Float64bits(float64(n+1) * 1.5)
		if got := runCompiledAmd64u(t, cm, n); got != want {
			t.Fatalf("f(%d) = %x, want %x", n, got, want)
		}
	}
}

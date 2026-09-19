//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestFloatConstCacheBranchExecAMD64(t *testing.T) {
	// A returning host import sets syncHostCalls for the whole module and
	// disables preload even in this call-free function (the #665 mechanism).
	// Previously operandRegF installed 100 in the then arm, so the else arm
	// read the persistent register without executing its initialization.
	body := []byte{0x00, 0x20, 0x00, 0x04, 0x7c} // local.get 0; if (result f64)
	arm := []byte{0x20, 0x01, 0x44}              // local.get 1; f64.const 100
	arm = binary.LittleEndian.AppendUint64(arm, math.Float64bits(100))
	arm = append(arm, 0xa2) // f64.mul
	body = append(body, arm...)
	body = append(body, 0x05) // else
	body = append(body, arm...)
	body = append(body, 0x0b, 0xbd, 0x0b) // end; i64.reinterpret_f64; end
	params, results := []wasm.ValType{wasm.I32, wasm.F64}, []wasm.ValType{wasm.I64}
	for _, imported := range []bool{false, true} {
		name := "preloaded"
		m := mod1(t, params, results, body)
		if imported {
			name = "unused-returning-import"
			m = hostSyncModule(wasmtest.FuncType(params, results), body)
		}
		t.Run(name, func(t *testing.T) {
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			for _, branch := range []uint64{0, 1, 0} {
				if got, want := runCompiledAmd64u(t, cm, branch, math.Float64bits(2)), math.Float64bits(200); got != want {
					t.Errorf("branch %d: result bits = %#x, want %#x", branch, got, want)
				}
			}
		})
	}
}

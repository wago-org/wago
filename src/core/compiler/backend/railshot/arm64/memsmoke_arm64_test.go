//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func modMem(t *testing.T, pages uint32, params, results []wasm.ValType, funcBody []byte) *wasm.Module {
	t.Helper()
	entry := append(wasmtest.ULEB(uint32(len(funcBody))), funcBody...)
	memType := append([]byte{0x00}, wasmtest.ULEB(pages)...) // flags=0 (min only)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memType)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// TestCompileMemory exercises the ported linear-memory codegen path (bounds check
// + the base+index+disp address fold) under qemu: it compiles functions that
// store to and load from linear memory at a nonzero memarg offset, asserting the
// backend produces non-empty AArch64 code without panicking. (Execution needs the
// JobMemory/basedata runtime, which is a separate task; this verifies codegen.)
func TestCompileMemory(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	cases := []struct {
		name string
		body []byte
	}{
		{
			// f(addr): mem[addr+4] = 42; return mem[addr+4]
			"store_load_off4",
			[]byte{
				0x00,
				0x20, 0x00, 0x41, 0x2a, 0x36, 0x02, 0x04, // local.get 0; i32.const 42; i32.store offset=4
				0x20, 0x00, 0x28, 0x02, 0x04, // local.get 0; i32.load offset=4
				0x0b,
			},
		},
		{
			// f(addr): return mem[addr]  (offset 0 — no disp fold)
			"load_off0",
			[]byte{0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b},
		},
		{
			// f(addr): mem8[addr+1] = mem8u[addr]  (sub-word load/store with disp)
			"i32_store8_off1",
			[]byte{
				0x00,
				0x20, 0x00, 0x20, 0x00, 0x2d, 0x00, 0x00, // local.get0; local.get0; i32.load8_u offset=0
				0x3a, 0x00, 0x01, // i32.store8 offset=1
				0x20, 0x00, 0x2d, 0x00, 0x00, // local.get0; i32.load8_u offset=0
				0x0b,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := modMem(t, 1, i32, i32, tc.body)
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatalf("CompileModule: %v", err)
			}
			if len(cm.Code) == 0 {
				t.Fatal("empty code")
			}
			if len(cm.Code)%4 != 0 {
				t.Errorf("code length %d not a multiple of 4", len(cm.Code))
			}
		})
	}
}

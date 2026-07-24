//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// These focused cases pin the slot-width invariant exposed by wazero's huge
// mixed stack: consuming call operands must preserve the logical types and the
// two-slot width of a v128 value below them.
func TestWazeroPortV128BelowRegisterCalls(t *testing.T) {
	const lo, hi = uint64(0x0123456789abcdef), uint64(0xfedcba9876543210)
	vec := wazeroStackWidthV128Const(lo, hi)

	t.Run("integer register ABI", func(t *testing.T) {
		caller := append([]byte(nil), vec...)
		caller = append(caller, 0x41, 0x07, 0x10, 0x00, 0x1a, 0x0b)
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
				wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
				wasmtest.Code(caller),
			)),
		)
		assertWazeroStackWidthVector(t, mod, nil, lo, hi)
	})

	t.Run("mixed register ABI", func(t *testing.T) {
		caller := append([]byte(nil), vec...)
		caller = append(caller, 0x44)
		var bits [8]byte
		binary.LittleEndian.PutUint64(bits[:], 0x4008000000000000) // 3.0
		caller = append(caller, bits[:]...)
		caller = append(caller, 0x10, 0x00, 0x1a, 0x0b)
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}),
				wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
				wasmtest.Code(caller),
			)),
		)
		assertWazeroStackWidthVector(t, mod, nil, lo, hi)
	})

	t.Run("void host call", func(t *testing.T) {
		imp := append(wasmtest.Name("host"), wasmtest.Name("sink")...)
		imp = append(imp, 0x00, 0x00)
		caller := append([]byte(nil), vec...)
		caller = append(caller, 0x41, 0x2a, 0x10, 0x00, 0x0b)
		mod := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
				wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
			)),
			wasmtest.Section(2, wasmtest.Vec(imp)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller))),
		)
		calls := 0
		imports := Imports{"host.sink": HostFunc(func(_ HostModule, params, _ []uint64) {
			calls++
			if len(params) != 1 || AsI32(params[0]) != 42 {
				t.Errorf("host params = %v, want [42]", params)
			}
		})}
		assertWazeroStackWidthVector(t, mod, imports, lo, hi)
		if calls != 1 {
			t.Fatalf("host calls = %d, want 1", calls)
		}
	})
}

func wazeroStackWidthV128Const(lo, hi uint64) []byte {
	out := []byte{0xfd, 0x0c}
	var bits [16]byte
	binary.LittleEndian.PutUint64(bits[:8], lo)
	binary.LittleEndian.PutUint64(bits[8:], hi)
	return append(out, bits[:]...)
}

func assertWazeroStackWidthVector(t *testing.T, mod []byte, imports Imports, wantLo, wantHi uint64) {
	t.Helper()
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	got, err := in.Invoke("f")
	if err != nil || len(got) != 2 || got[0] != wantLo || got[1] != wantHi {
		t.Fatalf("f() = %#v, %v; want [%#x %#x]", got, err, wantLo, wantHi)
	}
}

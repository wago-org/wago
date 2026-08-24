//go:build arm64

package arm64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func asyncHostPinnedLocalModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	importSig := wasmtest.FuncType(nil, nil)
	localSig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0x00, 0x00)
	body := []byte{0x01, 0x08, 0x7f} // one local run: eight i32 locals
	for i := byte(0); i < 8; i++ {
		body = append(body,
			0x41, 10+i, // i32.const 10+i
			0x21, i, // local.set i
		)
	}
	body = append(body,
		0x10, 0x00, // call env.f (void async log path)
		0x20, 0x00, // local.get 0
	)
	for i := byte(1); i < 8; i++ {
		body = append(body, 0x20, i, 0x6a) // local.get i; i32.add
	}
	body = append(body, 0x0b)
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	binary := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(importSig, localSig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(binary)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func asyncHostPinnedFloatModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	importSig := wasmtest.FuncType(nil, nil)
	localSig := wasmtest.FuncType(nil, []wasm.ValType{wasm.F64})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0x00, 0x00)
	body := []byte{0x01, 0x08, 0x7c} // one local run: eight f64 locals
	for i := byte(0); i < 8; i++ {
		var bits [8]byte
		binary.LittleEndian.PutUint64(bits[:], math.Float64bits(float64(i+1)))
		body = append(body, 0x44) // f64.const
		body = append(body, bits[:]...)
		body = append(body, 0x21, i) // local.set i
	}
	body = append(body,
		0x10, 0x00, // call env.f (void async log path)
		0x20, 0x00, // local.get 0
	)
	for i := byte(1); i < 8; i++ {
		body = append(body, 0x20, i, 0xa0) // local.get i; f64.add
	}
	body = append(body, 0x0b)
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	binaryModule := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(importSig, localSig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(binaryModule)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestAsyncHostLogSpillsOnlyScratchPinnedLocalsARM64(t *testing.T) {
	t.Run("gp", func(t *testing.T) {
		stats := compileWithStats(t, asyncHostPinnedLocalModuleARM64(t), false).Funcs[0]
		if stats.PinnedLocals != 8 {
			t.Fatalf("pinned locals = %d, want 8", stats.PinnedLocals)
		}
		if got := stats.Peephole["call-local-store"]; got != 3 {
			t.Fatalf("async host-log local stores = %d, want 3 for X9-X11 only (all: %v)", got, stats.Peephole)
		}
		if got := stats.Peephole["call-local-reload-gp"]; got != 3 {
			t.Fatalf("async host-log local reloads = %d, want 3 for X9-X11 only (all: %v)", got, stats.Peephole)
		}
	})

	t.Run("fp", func(t *testing.T) {
		stats := compileWithStats(t, asyncHostPinnedFloatModuleARM64(t), false).Funcs[0]
		if stats.PinnedLocals != 8 {
			t.Fatalf("pinned locals = %d, want 8", stats.PinnedLocals)
		}
		if got := stats.Peephole["call-local-store"]; got != 0 {
			t.Fatalf("async host-log FP local stores = %d, want 0 (all: %v)", got, stats.Peephole)
		}
		if got := stats.Peephole["call-local-reload-fp"]; got != 0 {
			t.Fatalf("async host-log FP local reloads = %d, want 0 (all: %v)", got, stats.Peephole)
		}
	})
}

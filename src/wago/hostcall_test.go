package wago

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestIndependentInstanceExecutionBypassesProcessLease(t *testing.T) {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x10, 0x00, 0x0b} // call 0; end
	c, err := NewRuntimeConfig().Compile(returningImportModule(sig, body))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(7)
	})}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	done := make(chan error, 1)
	go func() {
		results, invokeErr := in.Invoke("g")
		if invokeErr == nil && (len(results) != 1 || AsI32(results[0]) != 7) {
			invokeErr = errors.New("unexpected independent invocation result")
		}
		done <- invokeErr
	}()
	select {
	case invokeErr := <-done:
		if invokeErr != nil {
			t.Fatalf("invoke: %v", invokeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("independent invocation waited for the process-wide native execution lease")
	}
}

func TestIndependentInstanceExecutionFallsBackForExportedState(t *testing.T) {
	tests := []struct {
		name   string
		module []byte
		export func(*Instance) error
	}{
		{
			name: "memory",
			module: wasmtest.Module(
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("state", 2, 0))),
			),
			export: func(in *Instance) error {
				_, err := in.ExportedMemory("state")
				return err
			},
		},
		{
			name: "table",
			module: wasmtest.Module(
				wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("state", 1, 0))),
			),
			export: func(in *Instance) error {
				_, err := in.ExportedTable("state")
				return err
			},
		},
		{
			name: "global",
			module: wasmtest.Module(
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("state", 3, 0))),
			),
			export: func(in *Instance) error {
				_, err := in.ExportedGlobalObject("state")
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled := MustCompile(test.module)
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if !in.usesIndependentExecution() {
				t.Fatal("fresh instance did not use independent execution")
			}
			if err := test.export(in); err != nil {
				t.Fatal(err)
			}
			if in.usesIndependentExecution() {
				t.Fatal("exported cross-instance state retained independent execution")
			}
		})
	}
}

// returningImportModule builds a module whose func 0 is an import of type `sig`
// (env.f) and func 1 (exported "g") has body `body`. Optional extra sections
// (e.g. a memory) are appended before the export section.
func returningImportModule(sig, body []byte, extra ...[]byte) []byte {
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0x00, 0x00) // func, type 0
	fnBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	secs := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
	}
	secs = append(secs, extra...)
	secs = append(secs,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(fnBody)),
	)
	return wasmtest.Module(secs...)
}

// TestSyncHostImportSlotForm binds a returning host import as a reflection-free
// HostFunc (the TinyGo-safe form) and runs it through the public API.
func TestSyncHostImportSlotForm(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b} // local.get 0; call 0; end
	c := MustCompile(returningImportModule(sig, body))
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] * 3 })}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g", I32(7))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 21 {
		t.Fatalf("g(7) = %d, want 21", AsI32(res[0]))
	}
}

func TestSyncHostImportV128SlotForm(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	inVec := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	outVec := V128{0xf0, 0xe1, 0xd2, 0xc3, 0xb4, 0xa5, 0x96, 0x87, 0x78, 0x69, 0x5a, 0x4b, 0x3c, 0x2d, 0x1e, 0x0f}
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128, wasm.I64}, []wasm.ValType{wasm.V128, wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x10, 0x00, 0x0b} // local.get 0,1,2; call 0; end
	c := MustCompile(returningImportModule(sig, body))
	if !c.dynamicImports || len(c.code) == 0 {
		t.Fatal("v128 host import should compile through dynamic dispatch")
	}
	calls := 0
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) {
		calls++
		if len(p) != 4 { // i32 + two v128 slots + i64
			t.Fatalf("params len = %d, want 4", len(p))
		}
		if AsI32(p[0]) != 7 || p[3] != 0x1122334455667788 {
			t.Fatalf("scalar params = (%d,%#x), want (7,0x1122334455667788)", AsI32(p[0]), p[3])
		}
		if got := hostV128FromSlots(p[1], p[2]); got != inVec {
			t.Fatalf("v128 param = % x, want % x", got, inVec)
		}
		r[0], r[1] = hostV128Slots(outVec)
		r[2] = I32(99)
	})}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	lo, hi := hostV128Slots(inVec)
	res, err := in.Invoke("g", I32(7), lo, hi, I64(0x1122334455667788))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host called %d times, want 1", calls)
	}
	if got := hostV128FromSlots(res[0], res[1]); got != outVec {
		t.Fatalf("v128 result = % x, want % x", got, outVec)
	}
	if AsI32(res[2]) != 99 {
		t.Fatalf("i32 result = %d, want 99", AsI32(res[2]))
	}
}

func TestSyncHostImportVoidV128UsesSyncPath(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	vec := V128{0xaa, 0xbb, 0xcc, 0xdd, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0x00, 0x00) // func, type 0
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x41, 0x07, 0x0b}))), // local.get 0; call 0; i32.const 7; end
	)
	c := MustCompile(mod)
	if !c.dynamicImports || len(c.code) == 0 {
		t.Fatal("void v128 host import should compile through dynamic dispatch")
	}
	called := false
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) {
		called = true
		if got := hostV128FromSlots(p[0], p[1]); got != vec {
			t.Fatalf("v128 param = % x, want % x", got, vec)
		}
	})}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	lo, hi := hostV128Slots(vec)
	res, err := in.Invoke("g", lo, hi)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !called {
		t.Fatal("host function was not called")
	}
	if AsI32(res[0]) != 7 {
		t.Fatalf("g returned %d, want 7", AsI32(res[0]))
	}
}

func hostV128Slots(v V128) (uint64, uint64) {
	return binary.LittleEndian.Uint64(v[0:8]), binary.LittleEndian.Uint64(v[8:16])
}

func hostV128FromSlots(lo, hi uint64) V128 {
	var v V128
	binary.LittleEndian.PutUint64(v[0:8], lo)
	binary.LittleEndian.PutUint64(v[8:16], hi)
	return v
}

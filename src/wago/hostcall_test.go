package wago

import (
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestHostExitValueAndPointerThroughWasm(t *testing.T) {
	for _, value := range []any{HostExit{Code: 17}, &HostExit{Code: 17}} {
		c, err := NewRuntimeConfig().Compile(returningImportModule(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			[]byte{0x00, 0x10, 0x00, 0x0b},
		))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
			"env.f": HostFunc(func(HostModule, []uint64, []uint64) { panic(value) }),
		}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = in.Close() })
		_, err = in.Invoke("g")
		var exit *ExitError
		if !errors.As(err, &exit) || exit.Code != 17 {
			t.Fatalf("%T: got %v, want ExitError(17)", value, err)
		}
	}
}

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

func TestIndependentHostDispatchDoesNotRaceProcessEpoch(t *testing.T) {
	sig := wasmtest.FuncType(nil, nil)
	body := []byte{0x00, 0x10, 0x00, 0x0b} // call 0; end
	module := returningImportModule(sig, body)
	instantiate := func(config *RuntimeConfig) *Instance {
		compiled, err := config.Compile(module)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		t.Cleanup(func() { _ = compiled.Close() })
		in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(HostModule, []uint64, []uint64) {})}})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		t.Cleanup(func() { _ = in.Close() })
		return in
	}

	independent := instantiate(NewRuntimeConfig().WithIndependentInstanceExecution(true))
	serial := instantiate(NewRuntimeConfig().WithIndependentInstanceExecution(false))
	if !independent.usesIndependentExecution() || serial.usesIndependentExecution() {
		t.Fatal("test instances did not select distinct native execution leases")
	}

	const calls = 1000
	start := make(chan struct{})
	errs := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	run := func(in *Instance) {
		ready.Done()
		<-start
		for range calls {
			if _, err := in.Invoke("g"); err != nil {
				errs <- err
				return
			}
		}
		errs <- nil
	}
	go run(independent)
	go run(serial)
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
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

func TestTypedI32HostImports(t *testing.T) {
	t.Run("one parameter", func(t *testing.T) {
		sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
		body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b} // local.get 0; call 0; end
		c := MustCompile(returningImportModule(sig, body))
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
			"env.f": func(v int32) int32 { return v - 1 },
		}})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		defer in.Close()
		if got, err := in.Invoke("g", I32(-41)); err != nil || len(got) != 1 || AsI32(got[0]) != -42 {
			t.Fatalf("g(-41) = %v, %v; want -42", got, err)
		}
		if got := in.ensurePluginState().hostScope.sequence.Load(); got != 0 {
			t.Fatalf("typed callback issued %d Caller capabilities, want 0", got)
		}
	})

	t.Run("two parameters", func(t *testing.T) {
		sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
		body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b} // local.get 0, 1; call 0; end
		c := MustCompile(returningImportModule(sig, body))
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
			"env.f": I32I32ToI32HostFunc(func(a, b int32) int32 { return a + b }),
		}})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		defer in.Close()
		if got, err := in.Invoke("g", I32(20), I32(22)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			t.Fatalf("g(20, 22) = %v, %v; want 42", got, err)
		}
	})

	t.Run("panic", func(t *testing.T) {
		sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
		body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}
		c := MustCompile(returningImportModule(sig, body))
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
			"env.f": I32ToI32HostFunc(func(int32) int32 { panic(HostExit{Code: 23}) }),
		}})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		defer in.Close()
		_, err = in.Invoke("g", 0)
		var exit *ExitError
		if !errors.As(err, &exit) || exit.Code != 23 {
			t.Fatalf("typed callback panic = %v, want ExitError(23)", err)
		}
	})
}

func TestTypedI32HostImportBindingRejectsInvalidFunctions(t *testing.T) {
	oneI32 := FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}
	twoI32 := FuncSig{Params: []ValType{ValI32, ValI32}, Results: []ValType{ValI32}}
	one, err := bindSyncHostImport(I32ToI32HostFunc(func(v int32) int32 { return v }), oneI32)
	if err != nil || one.scalarKind != syncHostTypedI32 {
		t.Fatalf("one-parameter typed binding = kind %d, %v; want kind %d", one.scalarKind, err, syncHostTypedI32)
	}
	two, err := bindSyncHostImport(I32I32ToI32HostFunc(func(a, b int32) int32 { return a + b }), twoI32)
	if err != nil || two.scalarKind != syncHostTypedI32x2 {
		t.Fatalf("two-parameter typed binding = kind %d, %v; want kind %d", two.scalarKind, err, syncHostTypedI32x2)
	}

	var nilOne I32ToI32HostFunc
	if _, err := bindSyncHostImport(nilOne, oneI32); err == nil {
		t.Fatal("nil typed host function was accepted")
	}
	if _, err := bindSyncHostImport(I32I32ToI32HostFunc(func(a, b int32) int32 { return a + b }), oneI32); err == nil {
		t.Fatal("two-parameter typed host function accepted a one-parameter signature")
	}
	if _, err := bindSyncHostImport(I32ToI32HostFunc(func(v int32) int32 { return v }), twoI32); err == nil {
		t.Fatal("one-parameter typed host function accepted a two-parameter signature")
	}
}

func TestTypedHostSignatureMatrixRejectsMismatches(t *testing.T) {
	cases := []struct {
		name string
		fn   any
		bad  FuncSig
	}{
		{name: "empty_to_empty", fn: NoArgsHostFunc(func() {}), bad: FuncSig{Params: []ValType{ValI32}}},
		{name: "i32_to_empty", fn: I32HostFunc(func(int32) {}), bad: FuncSig{}},
		{name: "i32_to_i32", fn: I32ToI32HostFunc(func(v int32) int32 { return v }), bad: FuncSig{Params: []ValType{ValI32}}},
		{name: "i32_i32_to_empty", fn: I32I32HostFunc(func(int32, int32) {}), bad: FuncSig{Params: []ValType{ValI32}}},
		{name: "i32_i32_to_i32", fn: I32I32ToI32HostFunc(func(a, b int32) int32 { return a + b }), bad: FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}},
		{name: "i32_to_i32_i32", fn: I32ToI32I32HostFunc(func(v int32) (int32, int32) { return v, v }), bad: FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}},
		{name: "i32_i32_to_i32_i32", fn: I32I32ToI32I32HostFunc(func(a, b int32) (int32, int32) { return a, b }), bad: FuncSig{Params: []ValType{ValI32, ValI32}, Results: []ValType{ValI32}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := bindSyncHostImport(tc.fn, tc.bad); err == nil {
				t.Fatal("mismatched typed host signature was accepted")
			}
		})
	}
}

func TestGatedTypedI32HostImportAdmission(t *testing.T) {
	gate := newPluginCallGate("typed")
	binding, err := bindSyncHostImport(gatedI32ToI32HostFunc{
		fn:   func(v int32) int32 { return v + 1 },
		gate: gate,
	}, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	results := []uint64{0}
	dispatchSyncHostScalar(nil, nil, &binding, []uint64{41}, results, hostInvocationContext{})
	if results[0] != 42 || gate.state.Load() != 0 {
		t.Fatalf("gated typed call = %v, gate state %#x; want [42], 0", results, gate.state.Load())
	}

	reservation, err := reservePluginOperation([]*pluginCallGate{gate})
	if err != nil {
		t.Fatal(err)
	}
	gate.state.Store(pluginCallGateClosed | 1)
	results[0] = 0
	dispatchSyncHostScalar(nil, nil, &binding, []uint64{41}, results, hostInvocationContext{reservation: reservation})
	if results[0] != 42 {
		t.Fatalf("reserved typed call during close = %v, want [42]", results)
	}
	reservation.release()
	select {
	case <-gate.drained:
	default:
		t.Fatal("typed plugin gate did not drain after its reservation")
	}
}

func TestSyncHostImportV128SlotFormRejected(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128, wasm.I64}, []wasm.ValType{wasm.V128, wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x10, 0x00, 0x0b} // local.get 0,1,2; call 0; end
	c := MustCompile(returningImportModule(sig, body))
	if _, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(HostModule, []uint64, []uint64) {})}}); err == nil || !strings.Contains(err.Error(), "v128 host callbacks are not supported") {
		t.Fatalf("instantiate error = %v, want unsupported v128 host callback", err)
	}
}

func TestSyncHostImportVoidV128Rejected(t *testing.T) {
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
	if _, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(HostModule, []uint64, []uint64) {})}}); err == nil || !strings.Contains(err.Error(), "v128 host callbacks are not supported") {
		t.Fatalf("instantiate error = %v, want unsupported v128 host callback", err)
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

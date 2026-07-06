package wago

import (
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func importEntry(module, name string, kind byte, typeIdx uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, kind)
	return append(out, wasmtest.ULEB(typeIdx)...)
}

func voidI32ImportCallerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "log", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))), // local.get 0; call 0; end
	)
}

func voidF64ImportCallerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.F64}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf4, 0x3f, // f64.const 1.25
			0x10, 0x00, 0x0b, // call 0; end
		}))),
	)
}

func importedStartModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "start", 0, 0))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
	)
}

func returningI32Sig() []byte {
	return wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
}

func v128ResultSig() []byte { return wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}) }

func tableHostImportModule(sig []byte, body []byte) []byte {
	return tableHostImportModuleWithLocal(sig, sig, body)
}

func tableHostImportModuleWithLocal(importSig, localSig []byte, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(importSig, localSig)),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})), // funcref table min=1
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})), // elem (i32.const 0) [0]
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestVoidSyncHostFuncImportRunsOnce(t *testing.T) {
	c := MustCompile(voidI32ImportCallerModule())
	calls := 0
	in, err := Instantiate(c, Imports{"env.log": SyncHostFunc(func(_ HostModule, p, _ []uint64) {
		calls++
		if AsI32(p[0]) != 123 {
			t.Fatalf("param = %d, want 123", AsI32(p[0]))
		}
	})})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("g", I32(123)); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host called %d times, want 1", calls)
	}
}

func TestLegacyHostFuncImportStillRuns(t *testing.T) {
	c := MustCompile(voidI32ImportCallerModule())
	calls := 0
	in, err := Instantiate(c, Imports{"env.log": HostFunc(func(v int32) {
		calls++
		if v != 77 {
			t.Fatalf("param = %d, want 77", v)
		}
	})})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("g", I32(77)); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host called %d times, want 1", calls)
	}
}

func TestImportedStartSyncHostFuncRuns(t *testing.T) {
	c := MustCompile(importedStartModule())
	calls := 0
	in, err := Instantiate(c, Imports{"env.start": SyncHostFunc(func(_ HostModule, p, r []uint64) {
		calls++
		if len(p) != 0 || len(r) != 0 {
			t.Fatalf("start got params/results len %d/%d, want 0/0", len(p), len(r))
		}
	})})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if calls != 1 {
		t.Fatalf("start called %d times, want 1", calls)
	}
}

func TestImportedStartBadSignatureErrors(t *testing.T) {
	c := MustCompile(importedStartModule())
	_, err := Instantiate(c, Imports{"env.start": func(int32) {}})
	want := "takes 1 wasm params"
	if goruntime.Compiler == "tinygo" {
		want = "native-function host imports need standard Go"
	}
	if err == nil || !strings.Contains(err.Error(), "env.start") || !strings.Contains(err.Error(), want) {
		t.Fatalf("want clear start binding error containing %q, got %v", want, err)
	}
}

func TestMissingLegacyAsyncHostImportErrors(t *testing.T) {
	c := MustCompile(voidI32ImportCallerModule())
	_, err := Instantiate(c, nil)
	if err == nil || !strings.Contains(err.Error(), "env.log") || !strings.Contains(err.Error(), "legacy async host calls require wago.HostFunc") {
		t.Fatalf("want missing legacy host import error, got %v", err)
	}
}

func TestBindHostImportRejectsNilSlotForms(t *testing.T) {
	var sf SyncHostFunc
	if _, err := bindHostImport(sf, FuncSig{}); err == nil || !strings.Contains(err.Error(), "host function is nil") {
		t.Fatalf("want nil SyncHostFunc error, got %v", err)
	}
	var lf HostFunc
	if _, err := bindHostImport(lf, FuncSig{}); err == nil || !strings.Contains(err.Error(), "host function is nil") {
		t.Fatalf("want nil HostFunc error, got %v", err)
	}
}

func TestLegacyHostFuncRejectsIncompatibleSignatures(t *testing.T) {
	c := MustCompile(voidF64ImportCallerModule())
	_, err := Instantiate(c, Imports{"env.f": HostFunc(func(int32) {})})
	if err == nil || !strings.Contains(err.Error(), "legacy HostFunc only supports void imports") {
		t.Fatalf("want f64-param HostFunc rejection, got %v", err)
	}

	c = MustCompile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
	_, err = Instantiate(c, Imports{"env.f": HostFunc(func(int32) {})})
	if err == nil || !strings.Contains(err.Error(), "legacy HostFunc only supports void imports") {
		t.Fatalf("want returning HostFunc rejection, got %v", err)
	}

	if hostSupportsSIMD() {
		sig := wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128})
		body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b} // local.get 0; call 0; end
		c = MustCompile(returningImportModule(sig, body))
		_, err = Instantiate(c, Imports{"env.f": HostFunc(func(int32) {})})
		if err == nil || !strings.Contains(err.Error(), "legacy HostFunc only supports void imports") {
			t.Fatalf("want v128 HostFunc rejection, got %v", err)
		}
	}
}

func TestLegacyHostFuncSignatureValidationSurvivesCompiledRoundTrip(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	c := MustCompile(voidF64ImportCallerModule())
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary f64 import: %v", err)
	}
	var dec Compiled
	if err := dec.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary f64 import: %v", err)
	}
	_, err = Instantiate(&dec, Imports{"env.f": HostFunc(func(int32) {})})
	if err == nil || !strings.Contains(err.Error(), "legacy HostFunc only supports void imports") {
		t.Fatalf("want round-tripped f64 HostFunc rejection, got %v", err)
	}
}

func TestLegacyHostFuncCompatibleImportRoundTrips(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	c := MustCompile(voidI32ImportCallerModule())
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary i32 import: %v", err)
	}
	var dec Compiled
	if err := dec.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary i32 import: %v", err)
	}
	calls := 0
	in, err := Instantiate(&dec, Imports{"env.log": HostFunc(func(v int32) {
		calls++
		if v != 123 {
			t.Fatalf("param = %d, want 123", v)
		}
	})})
	if err != nil {
		t.Fatalf("instantiate round-tripped i32 import: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("g", I32(123)); err != nil {
		t.Fatalf("invoke round-tripped i32 import: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host called %d times, want 1", calls)
	}
}

func TestSyncHostImportInTableRunsIndirectly(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x0b} // local.get 0; i32.const 0; call_indirect type 0 table 0; end
	c := MustCompile(tableHostImportModule(sig, body))
	calls := 0
	in, err := Instantiate(c, Imports{"env.f": SyncHostFunc(func(_ HostModule, p, r []uint64) {
		calls++
		r[0] = p[0] + 1
	})})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g", I32(41))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 42 || calls != 1 {
		t.Fatalf("g/calls = %d/%d, want 42/1", AsI32(res[0]), calls)
	}
}

func TestVoidSyncHostImportInTableRunsIndirectly(t *testing.T) {
	importSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	localSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x41, 0x09, 0x0b} // call_indirect; i32.const 9; end
	c := MustCompile(tableHostImportModuleWithLocal(importSig, localSig, body))
	calls := 0
	in, err := Instantiate(c, Imports{"env.f": SyncHostFunc(func(_ HostModule, p, _ []uint64) {
		calls++
		if AsI32(p[0]) != 6 {
			t.Fatalf("param = %d, want 6", AsI32(p[0]))
		}
	})})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g", I32(6))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 9 || calls != 1 {
		t.Fatalf("g/calls = %d/%d, want 9/1", AsI32(res[0]), calls)
	}
}

func TestLegacyHostFuncInTableStillRunsIndirectly(t *testing.T) {
	importSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	localSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x41, 0x07, 0x0b} // call_indirect; i32.const 7; end
	c := MustCompile(tableHostImportModuleWithLocal(importSig, localSig, body))
	calls := 0
	in, err := Instantiate(c, Imports{"env.f": HostFunc(func(v int32) {
		calls++
		if v != 5 {
			t.Fatalf("param = %d", v)
		}
	})})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g", I32(5))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 7 || calls != 1 {
		t.Fatalf("g/calls = %d/%d, want 7/1", AsI32(res[0]), calls)
	}
}

func TestSyncHostImportV128InTableRunsIndirectly(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	inVec := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	outVec := V128{15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}
	sig := wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128})
	body := []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x0b} // local.get 0; i32.const 0; call_indirect type 0 table 0; end
	c := MustCompile(tableHostImportModule(sig, body))
	calls := 0
	in, err := Instantiate(c, Imports{"env.f": SyncHostFunc(func(_ HostModule, p, r []uint64) {
		calls++
		if got := hostV128FromSlots(p[0], p[1]); got != inVec {
			t.Fatalf("v128 param = % x, want % x", got, inVec)
		}
		r[0], r[1] = hostV128Slots(outVec)
	})})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	lo, hi := hostV128Slots(inVec)
	res, err := in.Invoke("g", lo, hi)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got := hostV128FromSlots(res[0], res[1]); got != outVec || calls != 1 {
		t.Fatalf("g/calls = % x/%d, want % x/1", got, calls, outVec)
	}
}

func TestMissingSyncHostDispatchErrors(t *testing.T) {
	in := &Instance{syncHosts: []SyncHostFunc{nil}}
	in.hostCall = in.newHostDispatch()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("missing host dispatch did not panic")
		} else if _, ok := r.(missingHostFunc); !ok {
			t.Fatalf("panic = %T %[1]v, want missingHostFunc", r)
		}
	}()
	var res [1]uint64
	in.hostCall(0, nil, res[:])
}

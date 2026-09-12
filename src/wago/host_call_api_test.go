package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestHostCallSupportsEveryABIValueType(t *testing.T) {
	types := []ValType{
		ValI32, ValI64, ValF32, ValF64, ValV128,
		ValFuncRef, ValExternRef, ValExnRef, ValAnyRef, ValI31Ref,
	}
	vec := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	lo, hi := binary.LittleEndian.Uint64(vec[:8]), binary.LittleEndian.Uint64(vec[8:])
	args := []uint64{
		I32(-7), I64(-9), F32(1.25), F64(-2.5), lo, hi,
		11, 12, 0, 13, uint64(NewI31Ref(-3).bits),
	}
	results := make([]uint64, len(args))
	called := false
	binding, err := bindSyncHostImport(func(call HostCall) {
		called = true
		if call.ParamCount() != len(types) || call.ResultCount() != len(types) {
			t.Fatalf("counts = %d/%d", call.ParamCount(), call.ResultCount())
		}
		if call.I32(0) != -7 || call.I64(1) != -9 || call.F32(2) != 1.25 || call.F64(3) != -2.5 {
			t.Fatal("numeric parameter mismatch")
		}
		if call.V128(4) != vec || call.FuncRef(5).token != 11 || call.ExternRef(6).token != 12 ||
			!call.ExnRef(7).IsNull() || call.GCRef(8).token != 13 || call.I31Ref(9).Signed() != -3 {
			t.Fatal("vector/reference parameter mismatch")
		}
		call.SetI32(0, -7)
		call.SetI64(1, -9)
		call.SetF32(2, 1.25)
		call.SetF64(3, -2.5)
		call.SetV128(4, vec)
		call.SetFuncRef(5, FuncRef{token: 11})
		call.SetExternRef(6, ExternRef{token: 12})
		call.SetExnRef(7, ExnRef{})
		call.SetGCRef(8, GCRef{token: 13})
		call.SetI31Ref(9, NewI31Ref(-3))
	}, FuncSig{Params: types, Results: types})
	if err != nil {
		t.Fatal(err)
	}
	binding.call(instanceHostModule{}, args, results)
	if !called {
		t.Fatal("callback was not called")
	}
	for i := range args {
		if results[i] != args[i] {
			t.Fatalf("result slot %d = %#x, want %#x", i, results[i], args[i])
		}
	}
}

func TestHostCallMixedSignatureRuntime(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	vec := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	sig := wasmtest.FuncType(
		[]wasm.ValType{wasm.I32, wasm.V128, wasm.I64},
		[]wasm.ValType{wasm.V128, wasm.I32},
	)
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x10, 0x00, 0x0b}
	compiled := MustCompile(returningImportModule(sig, body))
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": func(call HostCall) {
			if call.I32(0) != 7 || call.V128(1) != vec || call.I64(2) != 9 {
				t.Fatal("mixed HostCall parameters were decoded incorrectly")
			}
			call.SetV128(0, vec)
			call.SetI32(1, 42)
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	lo, hi := hostV128Slots(vec)
	results, err := instance.Invoke("g", I32(7), lo, hi, I64(9))
	if err != nil {
		t.Fatal(err)
	}
	if got := hostV128FromSlots(results[0], results[1]); got != vec || AsI32(results[2]) != 42 {
		t.Fatalf("mixed HostCall results = %x/%d", got, AsI32(results[2]))
	}
}

func TestOrdinaryNumericFunctionsRuntime(t *testing.T) {
	tests := []struct {
		name string
		typ  wasm.ValType
		fn   any
		arg  uint64
		want uint64
	}{
		{"i64", wasm.I64, func(v int64) int64 { return v + 1 }, I64(41), I64(42)},
		{"f32", wasm.F32, func(v float32) float32 { return v + 1 }, F32(1.5), F32(2.5)},
		{"f64", wasm.F64, func(v float64) float64 { return v + 1 }, F64(1.5), F64(2.5)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sig := wasmtest.FuncType([]wasm.ValType{test.typ}, []wasm.ValType{test.typ})
			body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}
			compiled := MustCompile(returningImportModule(sig, body))
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.f": test.fn}})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			results, err := instance.Invoke("g", test.arg)
			if err != nil || len(results) != 1 || results[0] != test.want {
				t.Fatalf("result = %#v, %v; want %#x", results, err, test.want)
			}
		})
	}
}

func TestHostCallUsesLogicalV128Indexes(t *testing.T) {
	sig := FuncSig{
		Params:  []ValType{ValI32, ValV128, ValI64},
		Results: []ValType{ValI32, ValV128, ValI64},
	}
	call := HostCall{
		params:  []uint64{1, 2, 3, 4},
		results: make([]uint64, 4),
		sig:     &sig,
	}
	if lo, hi := call.RawParam(1); lo != 2 || hi != 3 {
		t.Fatalf("v128 slots = %#x/%#x", lo, hi)
	}
	if got := call.ParamSlots(); len(got) != 4 || got[0] != 1 || got[3] != 4 {
		t.Fatalf("parameter slots = %#v", got)
	}
	if got := call.I64(2); got != 4 {
		t.Fatalf("post-v128 i64 = %d", got)
	}
	call.ResultSlots()[0] = 7
	call.SetRawResult(1, 8, 9)
	call.SetI64(2, 10)
	if call.results[0] != 7 || call.results[1] != 8 || call.results[2] != 9 || call.results[3] != 10 {
		t.Fatalf("results = %#v", call.results)
	}
}

func TestHostCallExposesExactReferenceType(t *testing.T) {
	exact := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{
		Nullable: false,
		Heap:     HeapTypeDescriptor{Defined: true, TypeIndex: 3},
	}}
	sig := FuncSig{Params: []ValType{ValFuncRef}, Results: []ValType{ValFuncRef}}
	call := HostCall{
		params: []uint64{0}, results: []uint64{0}, sig: &sig,
		exact: &DefinedTypeDescriptor{Params: []ValueTypeDescriptor{exact}, Results: []ValueTypeDescriptor{exact}},
	}
	if got := call.ParamType(0); got != exact {
		t.Fatalf("exact parameter type = %+v, want %+v", got, exact)
	}
	if got := call.ResultType(0); got != exact {
		t.Fatalf("exact result type = %+v, want %+v", got, exact)
	}
}

func TestHostCallDispatchAllocations(t *testing.T) {
	binding, err := bindSyncHostImport(func(call HostCall) {
		call.SetI32(0, call.I32(0)+1)
	}, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	args, results := []uint64{I32(1)}, []uint64{0}
	if got := testing.AllocsPerRun(1000, func() { binding.call(instanceHostModule{}, args, results) }); got != 0 {
		t.Fatalf("HostCall dispatch allocations = %v", got)
	}
}

func TestHostCallPluginGateBinding(t *testing.T) {
	gate := newPluginCallGate("host-call")
	value, err := gateHostImport(func(call HostCall) {
		call.SetI32(0, call.I32(0)+1)
	}, gate)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := bindSyncHostImport(value, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	if binding.gate != gate {
		t.Fatal("HostCall plugin gate was not retained")
	}
	results := []uint64{0}
	sig := FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}
	dispatchSyncHostReference(nil, nil, 0, 0, &binding, sig, nil, nil, nil, []uint64{41}, results, hostInvocationContext{})
	if results[0] != 42 {
		t.Fatalf("gated HostCall binding = %v", results)
	}
	if state := gate.state.Load(); state != 0 {
		t.Fatalf("HostCall plugin gate leaked admission: %#x", state)
	}
	gate.deactivate()
	func() {
		defer func() {
			if _, ok := recover().(HostTrap); !ok {
				t.Fatal("inactive HostCall plugin gate did not fail closed")
			}
		}()
		dispatchSyncHostReference(nil, nil, 0, 0, &binding, sig, nil, nil, nil, []uint64{41}, results, hostInvocationContext{})
	}()
}

func TestCallerHostCallBinding(t *testing.T) {
	binding, err := bindSyncHostImport(func(caller Caller, call HostCall) {
		if caller.Memory() != nil {
			t.Fatal("zero test caller unexpectedly has memory")
		}
		call.SetI32(0, call.I32(0)+1)
	}, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	results := []uint64{0}
	binding.call(instanceHostModule{}, []uint64{41}, results)
	if results[0] != 42 {
		t.Fatalf("caller HostCall result = %v", results)
	}
}

func TestOrdinaryNumericFunctionsInferAndBind(t *testing.T) {
	tests := []struct {
		fn   any
		typ  ValType
		args int
	}{
		{func(v int64) int64 { return v }, ValI64, 1},
		{func(a, b int64) int64 { return a + b }, ValI64, 2},
		{func(v float32) float32 { return v }, ValF32, 1},
		{func(a, b float32) float32 { return a + b }, ValF32, 2},
		{func(v float64) float64 { return v }, ValF64, 1},
		{func(a, b float64) float64 { return a + b }, ValF64, 2},
	}
	for _, test := range tests {
		params, results, ok := inferredHostFuncSignature(test.fn)
		if !ok || len(params) != test.args || len(results) != 1 || results[0] != test.typ {
			t.Fatalf("signature for %T = %v -> %v, %v", test.fn, params, results, ok)
		}
		if _, err := bindSyncHostImport(test.fn, FuncSig{Params: params, Results: results}); err != nil {
			t.Fatalf("bind %T: %v", test.fn, err)
		}
	}
}

func TestSingleTypedScalarPortalSelection(t *testing.T) {
	in := &Instance{syncHosts: []syncHostBinding{{
		typedI32:   func(int32) int32 { return 0 },
		scalarKind: syncHostTypedI32,
	}}}
	if !in.hasSingleDirectTypedScalarHost() || in.hasSingleExpandedTypedScalarHost() {
		t.Fatal("single direct scalar import selected the wrong portal")
	}

	in.syncHosts[0] = syncHostBinding{typedNone: func() {}, scalarKind: syncHostTypedNone}
	if in.hasSingleDirectTypedScalarHost() || !in.hasSingleExpandedTypedScalarHost() {
		t.Fatal("single expanded scalar import selected the wrong portal")
	}

	in.syncHosts[0].gate = newPluginCallGate("gated")
	if in.hasSingleExpandedTypedScalarHost() {
		t.Fatal("gated import selected the single-import portal")
	}
	in.syncHosts[0].gate = nil
	in.syncHosts = append(in.syncHosts, syncHostBinding{typedNone: func() {}, scalarKind: syncHostTypedNone})
	if in.hasSingleExpandedTypedScalarHost() {
		t.Fatal("multi-import instance selected the single-import portal")
	}

	sig := FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}
	in.syncHosts = []syncHostBinding{{
		fn: HostCallFunc(func(HostCall) {}), sig: &sig, hostCall: true, scalarKind: syncHostScalar,
	}}
	if !in.hasSingleHostCallPortal() {
		t.Fatal("single capability-free HostCallFunc did not select its portal")
	}
	in.syncHosts[0].gate = newPluginCallGate("gated")
	if in.hasSingleHostCallPortal() {
		t.Fatal("gated HostCallFunc selected the single-import portal")
	}
	in.syncHosts[0].gate = nil
	in.syncHosts[0].scalarKind = syncHostNonScalar
	if in.hasSingleHostCallPortal() {
		t.Fatal("reference HostCallFunc selected the scalar portal")
	}
}

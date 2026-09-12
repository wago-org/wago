package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestHostCallSupportsScalarAndReferenceValueTypes(t *testing.T) {
	types := []ValType{
		ValI32, ValI64, ValF32, ValF64,
		ValFuncRef, ValExternRef, ValExnRef, ValAnyRef, ValI31Ref,
	}
	args := []uint64{
		I32(-7), I64(-9), F32(1.25), F64(-2.5),
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
		if call.FuncRef(4).token != 11 || call.ExternRef(5).token != 12 ||
			!call.ExnRef(6).IsNull() || call.GCRef(7).token != 13 || call.I31Ref(8).Signed() != -3 {
			t.Fatal("reference parameter mismatch")
		}
		call.SetI32(0, -7)
		call.SetI64(1, -9)
		call.SetF32(2, 1.25)
		call.SetF64(3, -2.5)
		call.SetFuncRef(4, FuncRef{token: 11})
		call.SetExternRef(5, ExternRef{token: 12})
		call.SetExnRef(6, ExnRef{})
		call.SetGCRef(7, GCRef{token: 13})
		call.SetI31Ref(8, NewI31Ref(-3))
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

func TestHostCallRejectsV128Signature(t *testing.T) {
	sig := wasmtest.FuncType(
		[]wasm.ValType{wasm.I32, wasm.V128, wasm.I64},
		[]wasm.ValType{wasm.V128, wasm.I32},
	)
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x10, 0x00, 0x0b}
	compiled := MustCompile(returningImportModule(sig, body))
	defer compiled.Close()
	_, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": func(HostCall) {},
	}})
	if err == nil || !strings.Contains(err.Error(), "v128 host callbacks are not supported") {
		t.Fatalf("Instantiate error = %v, want unsupported v128 host callback", err)
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

func TestOrdinaryV128HostFunctionIsUnsupported(t *testing.T) {
	fn := func(v V128) V128 { return v }
	if isHostCallback(fn) {
		t.Fatal("ordinary v128 function was recognized as a host callback")
	}
	if _, _, ok := inferredHostFuncSignature(fn); ok {
		t.Fatal("ordinary v128 function signature was inferred")
	}
	if _, err := bindSyncHostImport(fn, FuncSig{Params: []ValType{ValV128}, Results: []ValType{ValV128}}); err == nil || !strings.Contains(err.Error(), "v128 host callbacks are not supported") {
		t.Fatalf("bind error = %v, want unsupported v128 host callback", err)
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

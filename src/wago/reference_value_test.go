package wago

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestPublicReferenceValueTypesAndNulls(t *testing.T) {
	if got := ValFuncRef.String(); got != "funcref" {
		t.Fatalf("ValFuncRef.String() = %q, want funcref", got)
	}
	if got := ValExternRef.String(); got != "externref" {
		t.Fatalf("ValExternRef.String() = %q, want externref", got)
	}

	fr := NullFuncRef()
	if !fr.IsNull() {
		t.Fatal("NullFuncRef is not null")
	}
	fv := ValueFuncRef(fr)
	if fv.Type() != ValFuncRef || fv.Bits() != 0 || !fv.FuncRef().IsNull() {
		t.Fatalf("null funcref value = type %s bits %#x ref-null %v", fv.Type(), fv.Bits(), fv.FuncRef().IsNull())
	}

	er := NullExternRef()
	if !er.IsNull() {
		t.Fatal("NullExternRef is not null")
	}
	ev := ValueExternRef(er)
	if ev.Type() != ValExternRef || ev.Bits() != 0 || !ev.ExternRef().IsNull() {
		t.Fatalf("null externref value = type %s bits %#x ref-null %v", ev.Type(), ev.Bits(), ev.ExternRef().IsNull())
	}
}

func TestPublicReferenceTokensAreOpaqueUint64Values(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{name: "FuncRef", typ: reflect.TypeOf(FuncRef{})},
		{name: "ExternRef", typ: reflect.TypeOf(ExternRef{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.typ.Kind() != reflect.Struct || tc.typ.Size() != 8 {
				t.Fatalf("%s representation = kind %s size %d, want opaque 8-byte struct", tc.name, tc.typ.Kind(), tc.typ.Size())
			}
			for i := 0; i < tc.typ.NumField(); i++ {
				field := tc.typ.Field(i)
				if field.IsExported() {
					t.Fatalf("%s field %q is exported", tc.name, field.Name)
				}
				if field.Type.Kind() != reflect.Uint64 {
					t.Fatalf("%s field %q kind = %s, want opaque uint64 token (not a Go/native pointer)", tc.name, field.Name, field.Type.Kind())
				}
			}
		})
	}
}

func TestReferenceSignatureConversionPreservesTypes(t *testing.T) {
	c := &Compiled{
		NumImports: 0,
		Funcs: []FuncSig{{
			Params:  valTypesFromWasm([]wasm.ValType{wasm.FuncRef, wasm.ExternRef}),
			Results: valTypesFromWasm([]wasm.ValType{wasm.ExternRef, wasm.FuncRef}),
		}},
		Exports: map[string]int{"refs": 0},
	}
	params, results, err := c.Signature("refs")
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	if !reflect.DeepEqual(params, []ValType{ValFuncRef, ValExternRef}) {
		t.Fatalf("params = %v, want [funcref externref]", params)
	}
	if !reflect.DeepEqual(results, []ValType{ValExternRef, ValFuncRef}) {
		t.Fatalf("results = %v, want [externref funcref]", results)
	}
}

func TestTypedCallCarriesOpaqueExternRefTokensInWideSlots(t *testing.T) {
	c := MustCompile(referenceSlotIdentityModule())
	c.Funcs[0] = FuncSig{Params: []ValType{ValExternRef}, Results: []ValType{ValExternRef}}
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	ref, err := in.NewExternRef("opaque")
	if err != nil {
		t.Fatalf("NewExternRef: %v", err)
	}
	value := ValueExternRef(ref)
	out, err := in.Call(context.Background(), "id", value)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(out) != 1 || out[0].Type() != ValExternRef || out[0].ExternRef() != ref || out[0].ExternRef().IsNull() {
		t.Fatalf("Call result = %#v, want stable opaque externref", out)
	}
	if _, err := in.Call(context.Background(), "id", ValueFuncRef(FuncRef{token: value.Bits()})); err == nil || !strings.Contains(err.Error(), "got") {
		t.Fatalf("cross-reference type mismatch error = %v", err)
	}
}

func referenceSlotIdentityModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
	)
}

func TestFuncrefGlobalProducerRetentionLifecycle(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	producer, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	descriptor, ok := producer.localFuncrefDescriptor(0)
	if !ok {
		t.Fatal("producer has no local funcref descriptor")
	}
	g := newGlobal(ValFuncRef, descriptor, V128{}, true)
	if !g.retainProducerInstance(producer) {
		t.Fatal("funcref global did not retain its producer")
	}
	if !g.retainProducerInstance(producer) {
		t.Fatal("repeated retention rejected the current producer")
	}
	producer.lifeMu.Lock()
	refs := producer.resourceRefs
	producer.lifeMu.Unlock()
	if refs != 1 {
		t.Fatalf("resource roots = %d, want one deduplicated root", refs)
	}
	writeGlobalObject(g, ValFuncRef, 0)
	if g.retainProducerInstance(producer) {
		t.Fatal("null descriptor retained a producer")
	}
	producer.lifeMu.Lock()
	refs = producer.resourceRefs
	producer.lifeMu.Unlock()
	if refs != 0 {
		t.Fatalf("overwritten descriptor retained %d roots", refs)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("Close global: %v", err)
	}
}

func TestHostFuncRefValidationAndLifecycleHelpers(t *testing.T) {
	fn := HostFunc(func(HostModule, []uint64, []uint64) {})
	sig := FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI64}}
	if funcSigEqual(sig, FuncSig{}) || !funcSigEqual(sig, sig) {
		t.Fatal("function signature equality changed")
	}
	if _, err := (*Runtime)(nil).NewHostFuncRef(fn, sig); err == nil {
		t.Fatal("nil runtime accepted host function reference")
	}
	rt := NewRuntime()
	if _, err := rt.NewHostFuncRef(nil, sig); err == nil {
		t.Fatal("nil host function accepted")
	}
	h, err := rt.NewHostFuncRef(fn, sig)
	if err != nil {
		t.Fatal(err)
	}
	if got := rt.refStore.hostFuncRef(hostFuncRefDispatchBit | h.dispatchIndex); got != h {
		t.Fatalf("host function dispatch lookup = %p, want %p", got, h)
	}
	for _, dispatch := range []uint32{0, h.dispatchIndex, hostFuncRefDispatchBit, hostFuncRefDispatchBit | h.dispatchIndex + 1} {
		if got := rt.refStore.hostFuncRef(dispatch); got != nil {
			t.Errorf("host function dispatch %x resolved to %p", dispatch, got)
		}
	}
	if err := h.validateImport(rt.refStore, sig); err != nil {
		t.Fatalf("valid host reference import: %v", err)
	}
	if err := h.validateImport(rt.refStore, FuncSig{}); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("mismatched import = %v", err)
	}
	if err := h.attachImporter(rt.refStore, sig); err != nil {
		t.Fatal(err)
	}
	source := &Instance{refStore: rt.refStore}
	if gotSource, descriptor, ok := h.canonicalDescriptor(source, 44, sig); !ok || gotSource != source || descriptor != 44 {
		t.Fatalf("first canonical descriptor = %p, %d, %v", gotSource, descriptor, ok)
	}
	if gotSource, descriptor, ok := h.canonicalDescriptor(&Instance{refStore: rt.refStore}, 99, sig); !ok || gotSource != source || descriptor != 44 {
		t.Fatalf("reused canonical descriptor = %p, %d, %v", gotSource, descriptor, ok)
	}
	source.resourcesClosed = true
	if _, _, ok := h.canonicalDescriptor(source, 44, sig); ok {
		t.Fatal("closed descriptor source remained canonical")
	}
	source.resourcesClosed = false
	h.tokenReleased(source, 44)
	if err := h.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
		t.Fatalf("close with importer = %v", err)
	}
	h.detachImporter()
	if _, _, ok := h.canonicalDescriptor(nil, 1, sig); ok {
		t.Fatal("nil source produced a canonical descriptor")
	}
	if !h.markTokenLive(nil, 0) {
		t.Fatal("matching uninitialized token was not accepted")
	}
	h.tokenReleased(nil, 0)
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := bindHostImport(h, sig); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed host reference bound: %v", err)
	}
	if got, err := bindHostImport(fn, sig); err != nil || got == nil {
		t.Fatalf("direct host function binding = %v, %v", got, err)
	}
	for _, value := range []any{nil, HostFunc(nil), (*HostFuncRef)(nil), 3} {
		if _, err := bindHostImport(value, sig); err == nil {
			t.Fatalf("invalid host import %#v accepted", value)
		}
	}
	expired := instanceHostModule{}
	if expired.valid() || expired.Memory() != nil {
		t.Fatal("empty host module was valid")
	}
	if _, err := expired.NewExternRef("x"); err == nil {
		t.Fatal("expired host module created externref")
	}
	if _, ok := expired.ExternRefValue(ExternRef{}); ok {
		t.Fatal("expired host module resolved externref")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHostCallWaitRegistrationLifecycle(t *testing.T) {
	var scope hostCallScope
	h := scope.begin(&Instance{})
	w := &hostCallWaiter{wake: make(chan struct{}, 1)}
	if !h.registerWait(w) || w.generation != h.generation {
		t.Fatal("active host module did not register waiter")
	}
	scope.end(h.generation)
	select {
	case <-w.wake:
	default:
		t.Fatal("scope end did not wake registered waiter")
	}
	h.unregisterWait(w)
	if got := scope.waiter.Load(); got != nil {
		t.Fatalf("unregister retained waiter %p", got)
	}
	if h.registerWait(&hostCallWaiter{wake: make(chan struct{}, 1)}) {
		t.Fatal("expired host module registered waiter")
	}
	static := instanceHostModule{}
	if !static.registerWait(&hostCallWaiter{}) {
		t.Fatal("scope-free host module rejected waiter")
	}
	static.unregisterWait(&hostCallWaiter{})
}

func TestHostReferenceTranslationSlotAndTokenValidation(t *testing.T) {
	in := &Instance{}
	if err := in.translateHostReferenceArgs(nil, []ValType{ValV128}, nil); err != nil {
		t.Fatalf("v128 argument slots: %v", err)
	}
	if err := in.translateHostReferenceResults(nil, []ValType{ValV128}, nil); err != nil {
		t.Fatalf("v128 result slots: %v", err)
	}
	funcrefType := []ValueTypeDescriptor{{
		Kind: ValueTypeReference,
		Ref:  ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}},
	}}
	for _, tc := range []struct {
		name string
		fn   func() error
	}{
		{"missing argument", func() error { return in.translateHostReferenceArgs(nil, []ValType{ValI32}, nil) }},
		{"invalid externref argument", func() error { return in.translateHostReferenceArgs([]uint64{1}, []ValType{ValExternRef}, nil) }},
		{"missing result", func() error { return in.translateHostReferenceResults(nil, []ValType{ValI64}, nil) }},
		{"invalid funcref result", func() error { return in.translateHostReferenceResults([]uint64{1}, []ValType{ValFuncRef}, funcrefType) }},
		{"invalid externref result", func() error { return in.translateHostReferenceResults([]uint64{1}, []ValType{ValExternRef}, nil) }},
	} {
		if err := tc.fn(); err == nil {
			t.Errorf("%s accepted invalid host reference values", tc.name)
		}
	}
}

func TestHostFuncRefDescriptorLookup(t *testing.T) {
	owner := &HostFuncRef{}
	const key = "env.target"
	in := &Instance{
		c: &Compiled{
			NumImports: 1,
			Imports:    []string{key},
			FuncTypeID: []uint64{1, 1},
		},
		imports:      Imports{key: owner},
		funcRefDescs: make([]byte, 3*coreruntime.FuncRefDescBytes),
	}
	desc := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[coreruntime.FuncRefDescBytes])))
	if got := in.hostFuncRefForDescriptor(desc); got != owner {
		t.Fatalf("host descriptor owner = %p, want %p", got, owner)
	}
	for _, bad := range []uint64{
		0,
		uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[0]))),
		desc + 1,
		uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[2*coreruntime.FuncRefDescBytes]))),
	} {
		if got := in.hostFuncRefForDescriptor(bad); got != nil {
			t.Fatalf("invalid descriptor %#x resolved to %p", bad, got)
		}
	}
	if (&Instance{}).hostFuncRefForDescriptor(desc) != nil {
		t.Fatal("descriptor resolved without a descriptor arena")
	}
	local, ok := in.localFuncrefDescriptor(0)
	wantLocal := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[2*coreruntime.FuncRefDescBytes])))
	if !ok || local != wantLocal {
		t.Fatalf("local descriptor = %#x, %v; want %#x, true", local, ok, wantLocal)
	}
	if _, ok := in.localFuncrefDescriptor(-1); ok {
		t.Fatal("negative local descriptor index accepted")
	}
	if _, ok := in.localFuncrefDescriptor(1); ok {
		t.Fatal("out-of-range local descriptor index accepted")
	}
}

package wago

import (
	"strings"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

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
	if err := in.translateHostReferenceArgs(nil, []ValType{ValV128}); err != nil {
		t.Fatalf("v128 argument slots: %v", err)
	}
	if err := in.translateHostReferenceResults(nil, []ValType{ValV128}); err != nil {
		t.Fatalf("v128 result slots: %v", err)
	}
	for _, tc := range []struct {
		name string
		fn   func() error
	}{
		{"missing argument", func() error { return in.translateHostReferenceArgs(nil, []ValType{ValI32}) }},
		{"invalid externref argument", func() error { return in.translateHostReferenceArgs([]uint64{1}, []ValType{ValExternRef}) }},
		{"missing result", func() error { return in.translateHostReferenceResults(nil, []ValType{ValI64}) }},
		{"invalid funcref result", func() error { return in.translateHostReferenceResults([]uint64{1}, []ValType{ValFuncRef}) }},
		{"invalid externref result", func() error { return in.translateHostReferenceResults([]uint64{1}, []ValType{ValExternRef}) }},
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
			FuncTypeID: []uint32{1, 1},
		},
		imports:      Imports{key: owner},
		funcRefDescs: make([]byte, 3*coreruntime.TableEntryBytes),
	}
	desc := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[coreruntime.TableEntryBytes])))
	if got := in.hostFuncRefForDescriptor(desc); got != owner {
		t.Fatalf("host descriptor owner = %p, want %p", got, owner)
	}
	for _, bad := range []uint64{
		0,
		uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[0]))),
		desc + 1,
		uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[2*coreruntime.TableEntryBytes]))),
	} {
		if got := in.hostFuncRefForDescriptor(bad); got != nil {
			t.Fatalf("invalid descriptor %#x resolved to %p", bad, got)
		}
	}
	if (&Instance{}).hostFuncRefForDescriptor(desc) != nil {
		t.Fatal("descriptor resolved without a descriptor arena")
	}
	local, ok := in.localFuncrefDescriptor(0)
	wantLocal := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[2*coreruntime.TableEntryBytes])))
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

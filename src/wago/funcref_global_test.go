package wago

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestRelease2RefFuncGlobalInitializersWithoutTable(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	producerModule, err := rt.Compile(noTableRefFuncGlobalModule())
	if err != nil {
		t.Fatalf("Compile no-table ref.func global: %v", err)
	}
	if producerModule.c.HasTable {
		t.Fatal("no-table ref.func global unexpectedly requires a Wasm table")
	}
	consumerModule, err := rt.Compile(funcrefCallableConsumerModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerModule)
	if err != nil {
		t.Fatalf("Instantiate no-table ref.func global: %v", err)
	}
	defer producer.Close()
	consumer, err := rt.Instantiate(context.Background(), consumerModule)
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()

	fromGlobal, err := producer.Call(context.Background(), "get_global")
	if err != nil || len(fromGlobal) != 1 || fromGlobal[0].FuncRef().IsNull() {
		t.Fatalf("get_global = %v, %v; want one non-null funcref", fromGlobal, err)
	}
	fromBody, err := producer.Call(context.Background(), "get_direct")
	if err != nil || len(fromBody) != 1 || fromBody[0].Bits() != fromGlobal[0].Bits() {
		t.Fatalf("get_direct = %v, %v; want global token %#x", fromBody, err, fromGlobal[0].Bits())
	}
	if got, err := consumer.Call(context.Background(), "call", fromGlobal[0]); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("call(global ref.func) = %v, %v; want 42", got, err)
	}
	global, err := producer.GlobalValue("target_ref")
	if err != nil || global.Bits() != fromGlobal[0].Bits() {
		t.Fatalf("GlobalValue(target_ref) = %v, %v; want token %#x", global, err, fromGlobal[0].Bits())
	}
}

func TestRefFuncGlobalDescriptorArenaIsBoundedAndDemandDriven(t *testing.T) {
	withRef, err := Compile(nil, noTableRefFuncGlobalModule())
	if err != nil {
		t.Fatalf("Compile ref.func global: %v", err)
	}
	withNull, err := Compile(nil, nullableLocalFuncrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile nullable globals: %v", err)
	}
	refInstance, err := Instantiate(withRef)
	if err != nil {
		t.Fatalf("Instantiate ref.func global: %v", err)
	}
	defer refInstance.Close()
	nullInstance, err := Instantiate(withNull)
	if err != nil {
		t.Fatalf("Instantiate nullable globals: %v", err)
	}
	defer nullInstance.Close()

	want := (len(withRef.FuncTypeID) + 1) * coreruntime.FuncRefDescBytes
	if got := len(refInstance.funcRefDescs); got != want {
		t.Fatalf("ref.func descriptor arena = %d bytes, want exact bounded %d", got, want)
	}
	if got := len(nullInstance.funcRefDescs); got != 0 {
		t.Fatalf("null-only descriptor arena = %d bytes, want zero", got)
	}
}

func TestRefFuncGlobalHostImportEgressFailsClosed(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(hostImportedRefFuncGlobalModule())
	if err != nil {
		t.Fatalf("Compile host-imported ref.func global: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.target": HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(42)
	})}))
	if err != nil {
		t.Fatalf("Instantiate host-imported ref.func global: %v", err)
	}
	defer in.Close()

	got, err := in.Invoke("get")
	if err == nil || !strings.Contains(err.Error(), "invalid funcref result") || got != nil {
		t.Fatalf("host-imported global get = %v, %v; want fail-closed egress", got, err)
	}
	if len(rt.refStore.byToken) != 0 || len(rt.refStore.byDescriptor) != 0 {
		t.Fatal("host-imported global egress issued a public token")
	}
}

func TestRelease2NullableLocalFuncrefGlobals(t *testing.T) {
	c, err := Compile(nil, nullableLocalFuncrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile nullable local funcref globals: %v", err)
	}
	defer c.Close()

	for _, name := range []string{"immutable", "mutable"} {
		g, ok := c.ExportedGlobal(name)
		if !ok || g.Type != ValFuncRef || (name == "mutable") != g.Mutable {
			t.Fatalf("ExportedGlobal(%q) = %+v, %v", name, g, ok)
		}
	}

	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate nullable local funcref globals: %v", err)
	}
	defer in.Close()

	for _, name := range []string{"get_immutable", "get_mutable"} {
		out, err := in.Call(context.Background(), name)
		if err != nil {
			t.Fatalf("Call %s: %v", name, err)
		}
		if len(out) != 1 || out[0].Type() != ValFuncRef || !out[0].FuncRef().IsNull() {
			t.Fatalf("Call %s = %v, want one null funcref", name, out)
		}
	}

	out, err := in.Call(context.Background(), "set_and_get", ValueFuncRef(NullFuncRef()))
	if err != nil {
		t.Fatalf("Call set_and_get(null): %v", err)
	}
	if len(out) != 1 || out[0].Type() != ValFuncRef || !out[0].FuncRef().IsNull() {
		t.Fatalf("Call set_and_get(null) = %v, want one null funcref", out)
	}

	v, err := in.GlobalValue("mutable")
	if err != nil || v.Type() != ValFuncRef || !v.FuncRef().IsNull() {
		t.Fatalf("GlobalValue(mutable) = %v, %v; want null funcref", v, err)
	}
	if err := in.SetGlobalValue("mutable", ValueFuncRef(NullFuncRef())); err != nil {
		t.Fatalf("SetGlobalValue(mutable, null): %v", err)
	}
	if _, err := in.Global("mutable"); err == nil || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("Global(mutable) error = %v, want raw reference-global rejection", err)
	}
	if err := in.SetGlobal("mutable", 1); err == nil || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("SetGlobal(mutable, forged) error = %v, want raw reference-global rejection", err)
	}
	if err := in.SetGlobalValue("mutable", ValueOf(ValFuncRef, 1)); err == nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatalf("SetGlobalValue(mutable, forged) error = %v, want token rejection", err)
	}
}

func TestLocalFuncrefGlobalRoundTripRetainsProducer(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	producerModule, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	globalModule, err := rt.Compile(nullableLocalFuncrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile global module: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerModule)
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	global, err := rt.Instantiate(context.Background(), globalModule)
	if err != nil {
		t.Fatalf("Instantiate global module: %v", err)
	}
	defer global.Close()

	out, err := producer.Call(context.Background(), "get")
	if err != nil || len(out) != 1 || out[0].FuncRef().IsNull() {
		t.Fatalf("producer get = %v, %v; want non-null funcref", out, err)
	}
	token := out[0]

	roundTrip, err := global.Call(context.Background(), "set_and_get", token)
	if err != nil || len(roundTrip) != 1 || roundTrip[0].Bits() != token.Bits() {
		t.Fatalf("set_and_get(token) = %v, %v; want token %#x", roundTrip, err, token.Bits())
	}
	if err := global.SetGlobalValue("mutable", token); err != nil {
		t.Fatalf("SetGlobalValue(mutable, token): %v", err)
	}
	got, err := global.GlobalValue("mutable")
	if err != nil || got.Bits() != token.Bits() {
		t.Fatalf("GlobalValue(mutable) = %v, %v; want token %#x", got, err, token.Bits())
	}
	raw, err := global.ExportedGlobalObject("mutable")
	if err != nil {
		t.Fatalf("ExportedGlobalObject(mutable): %v", err)
	}
	if bits := raw.Get(); bits != 0 {
		t.Fatalf("reference Global.Get() = %#x, want fail-closed zero", bits)
	}
	if err := raw.Set(token.Bits()); err == nil || !strings.Contains(err.Error(), "reference type") {
		t.Fatalf("reference Global.Set(token) error = %v, want typed-accessor rejection", err)
	}

	if err := producer.Close(); err != nil {
		t.Fatalf("Close producer: %v", err)
	}
	got, err = global.GlobalValue("mutable")
	if err != nil || got.Bits() != token.Bits() {
		t.Fatalf("GlobalValue after producer close = %v, %v; want retained token %#x", got, err, token.Bits())
	}
}

func TestNullableLocalFuncrefGlobalsRespectFeatureAndOwnershipBoundaries(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	if _, err := Compile(cfg, nullableLocalFuncrefGlobalsModule()); err == nil || !strings.Contains(err.Error(), "reference-types disabled") {
		t.Fatalf("Compile with reference types disabled error = %v, want reference-types gate", err)
	}

	imported := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))))
	compiled, err := Compile(nil, imported)
	if err != nil {
		t.Fatalf("Compile imported funcref global: %v", err)
	}
	defer compiled.Close()
	if _, err := Instantiate(compiled, Imports{"env.ref": GlobalImport{Type: ValFuncRef}}); err == nil || !strings.Contains(err.Error(), "explicit store-bound *Global") {
		t.Fatalf("Instantiate unowned imported funcref global error = %v", err)
	}
}

func TestInstantiateRejectsUnsupportedReferenceGlobalMetadata(t *testing.T) {
	tests := []struct {
		name string
		c    *Compiled
		want string
	}{
		{name: "non-structural initializer bits", c: &Compiled{Globals: []GlobalDef{{Type: ValFuncRef, Bits: 1}}}, want: "non-structural funcref global initializer"},
		{name: "non-null externref initializer", c: &Compiled{Globals: []GlobalDef{{Type: ValExternRef, Bits: 1}}}, want: "non-null externref global initializer"},
		{name: "ref.func wrong type", c: &Compiled{Globals: []GlobalDef{{Type: ValI64, HasInitFunc: true}}, NeedsFuncRefDescs: true}, want: "ref.func initializer has type i64"},
		{name: "ref.func out of range", c: &Compiled{Code: []byte{0}, Entry: []int{0}, Funcs: []FuncSig{{}}, FuncTypeID: []uint32{0}, Globals: []GlobalDef{{Type: ValFuncRef, HasInitFunc: true, InitFunc: 1}}, NeedsFuncRefDescs: true}, want: "ref.func initializer index 1 out of range"},
		{name: "ref.func without descriptors", c: &Compiled{Code: []byte{0}, Entry: []int{0}, Funcs: []FuncSig{{}}, FuncTypeID: []uint32{0}, Globals: []GlobalDef{{Type: ValFuncRef, HasInitFunc: true}}}, want: "ref.func initializer without descriptor arena"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Instantiate(tc.c); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Instantiate error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNullableLocalFuncrefGlobalsRemainOutOfSerializedState(t *testing.T) {
	c := compileExplicitArtifact(t, nullableLocalFuncrefGlobalsModule())
	defer c.Close()

	_ = roundTripCompiled(t, c)
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "reference global metadata") {
		t.Fatalf("Capture error = %v, want reference-global rejection", err)
	}
}

func TestRelease2NullableFuncrefGlobalSourceGuard(t *testing.T) {
	if !requireStandardGoTestRuntime(t) {
		return
	}
	path := filepath.Clean("../../tests/spec-v2/test/core/linking.wast")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("Release 2 linking fixture unavailable: %v", err)
	}
	text := string(b)
	for _, declaration := range []string{
		`(global (export "g-const-func") funcref (ref.null func))`,
		`(global (export "g-var-func") (mut funcref) (ref.null func))`,
	} {
		if !strings.Contains(text, declaration) {
			t.Fatalf("Release 2 linking fixture no longer contains %q", declaration)
		}
	}
}

func hostImportedRefFuncGlobalModule() []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("target")...)
	imp = append(imp, 0x00, 0x00) // function import, type 0
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0x00, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
	)
}

func noTableRefFuncGlobalModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0x00, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get_global", 0, 1),
			wasmtest.ExportEntry("get_direct", 0, 2),
			wasmtest.ExportEntry("target_ref", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
		)),
	)
}

// nullableLocalFuncrefGlobalsModule isolates the funcref half of the official
// Release 2 linking.wast lines 97-98 declarations and adds function-level
// global.get/global.set execution. Imported globals, externref globals, and
// non-null ref.func initializers remain separate ownership slices.
func nullableLocalFuncrefGlobalsModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd0, 0x70, 0x0b}),
			wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get_immutable", 0, 0),
			wasmtest.ExportEntry("get_mutable", 0, 1),
			wasmtest.ExportEntry("set_and_get", 0, 2),
			wasmtest.ExportEntry("immutable", 3, 0),
			wasmtest.ExportEntry("mutable", 3, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x23, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x01, 0x23, 0x01, 0x0b}),
		)),
	)
}

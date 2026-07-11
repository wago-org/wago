package wago

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestStoreBoundExternrefGlobalImportsShareExactState(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	initial := issueExternref(t, rt, "initial")
	shared, err := rt.NewExternRefGlobal(initial, true)
	if err != nil {
		t.Fatalf("NewExternRefGlobal: %v", err)
	}
	defer shared.Close()

	mod, err := rt.Compile(importedReferenceGlobalModule(wasm.ExternRef, true))
	if err != nil {
		t.Fatalf("Compile imported externref global: %v", err)
	}
	first, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.ref": shared}))
	if err != nil {
		t.Fatalf("Instantiate first importer: %v", err)
	}
	defer first.Close()
	second, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.ref": shared}))
	if err != nil {
		t.Fatalf("Instantiate second importer: %v", err)
	}
	defer second.Close()
	reexport, err := first.ExportedGlobalObject("ref")
	if err != nil || reexport != shared {
		t.Fatalf("imported global re-export = %p, %v; want exact handle %p", reexport, err, shared)
	}

	for _, in := range []*Instance{first, second} {
		out, err := in.Call(context.Background(), "get")
		if err != nil || len(out) != 1 || out[0].ExternRef() != initial {
			t.Fatalf("initial get = %v, %v; want shared externref", out, err)
		}
	}
	next := issueExternref(t, rt, "next")
	out, err := first.Call(context.Background(), "set_and_get", ValueExternRef(next))
	if err != nil || len(out) != 1 || out[0].ExternRef() != next {
		t.Fatalf("set_and_get(next) = %v, %v", out, err)
	}
	out, err = second.Call(context.Background(), "get")
	if err != nil || len(out) != 1 || out[0].ExternRef() != next {
		t.Fatalf("second get after first write = %v, %v", out, err)
	}
	value, err := shared.GetValue()
	if err != nil || value.ExternRef() != next {
		t.Fatalf("host GetValue = %v, %v; want next", value, err)
	}
	if err := shared.SetValue(ValueExternRef(initial)); err != nil {
		t.Fatalf("host SetValue(initial): %v", err)
	}
	out, err = first.Call(context.Background(), "get")
	if err != nil || len(out) != 1 || out[0].ExternRef() != initial {
		t.Fatalf("first get after host write = %v, %v", out, err)
	}
}

func TestLocalReferenceGlobalExportsReimportAndRetainProducer(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	callableMod, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatalf("Compile callable producer: %v", err)
	}
	globalMod, err := rt.Compile(nullableLocalFuncrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile global producer: %v", err)
	}
	consumerMod, err := rt.Compile(importedReferenceGlobalModule(wasm.FuncRef, true))
	if err != nil {
		t.Fatalf("Compile funcref importer: %v", err)
	}
	callable, err := rt.Instantiate(context.Background(), callableMod)
	if err != nil {
		t.Fatalf("Instantiate callable producer: %v", err)
	}
	globalProducer, err := rt.Instantiate(context.Background(), globalMod)
	if err != nil {
		t.Fatalf("Instantiate global producer: %v", err)
	}
	token, err := callable.Call(context.Background(), "get")
	if err != nil || len(token) != 1 || token[0].FuncRef().IsNull() {
		t.Fatalf("producer get = %v, %v", token, err)
	}
	if err := globalProducer.SetGlobalValue("mutable", token[0]); err != nil {
		t.Fatalf("seed shared funcref global: %v", err)
	}
	shared, err := globalProducer.ExportedGlobalObject("mutable")
	if err != nil {
		t.Fatalf("ExportedGlobalObject: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerMod, WithImports(Imports{"env.ref": shared}))
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()
	if err := globalProducer.Close(); err != nil {
		t.Fatalf("Close global producer: %v", err)
	}
	if globalProducer.resourcesClosed {
		t.Fatal("global producer resources closed while an importer remained live")
	}
	out, err := consumer.Call(context.Background(), "get")
	if err != nil || len(out) != 1 || out[0].Bits() != token[0].Bits() {
		t.Fatalf("consumer get after producer close = %v, %v; want token %#x", out, err, token[0].Bits())
	}
	if err := callable.Close(); err != nil {
		t.Fatalf("Close callable producer: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("Close consumer: %v", err)
	}
	if !globalProducer.resourcesClosed {
		t.Fatal("global producer resources remained after final importer close")
	}
}

func TestImportedReferenceGlobalGetInitializersPreserveIdentity(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	extern := issueExternref(t, rt, "initializer")
	hostGlobal, err := rt.NewExternRefGlobal(extern, false)
	if err != nil {
		t.Fatalf("NewExternRefGlobal: %v", err)
	}
	defer hostGlobal.Close()
	externMod, err := rt.Compile(importedGlobalGetInitializerModule(wasm.ExternRef))
	if err != nil {
		t.Fatalf("Compile externref global.get initializer: %v", err)
	}
	externIn, err := rt.Instantiate(context.Background(), externMod, WithImports(Imports{"env.ref": hostGlobal}))
	if err != nil {
		t.Fatalf("Instantiate externref initializer: %v", err)
	}
	defer externIn.Close()
	out, err := externIn.Call(context.Background(), "get")
	if err != nil || len(out) != 1 || out[0].ExternRef() != extern {
		t.Fatalf("externref initializer get = %v, %v", out, err)
	}

	producerMod, err := rt.Compile(noTableRefFuncGlobalModule())
	if err != nil {
		t.Fatalf("Compile funcref producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatalf("Instantiate funcref producer: %v", err)
	}
	funcrefGlobal, err := producer.ExportedGlobalObject("target_ref")
	if err != nil {
		t.Fatalf("Export funcref global: %v", err)
	}
	funcrefMod, err := rt.Compile(importedGlobalGetInitializerModule(wasm.FuncRef))
	if err != nil {
		t.Fatalf("Compile funcref global.get initializer: %v", err)
	}
	funcrefIn, err := rt.Instantiate(context.Background(), funcrefMod, WithImports(Imports{"env.ref": funcrefGlobal}))
	if err != nil {
		t.Fatalf("Instantiate funcref initializer: %v", err)
	}
	defer funcrefIn.Close()
	want, err := producer.Call(context.Background(), "get_global")
	if err != nil || len(want) != 1 {
		t.Fatalf("producer get_global = %v, %v", want, err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close funcref producer: %v", err)
	}
	out, err = funcrefIn.Call(context.Background(), "get")
	if err != nil || len(out) != 1 || out[0].Bits() != want[0].Bits() {
		t.Fatalf("funcref initializer after producer close = %v, %v; want %#x", out, err, want[0].Bits())
	}
}

func TestReferenceGlobalImportRejectsTypeMutabilityStoreAndForgedValues(t *testing.T) {
	rtA, rtB := NewRuntime(), NewRuntime()
	defer rtA.Close()
	defer rtB.Close()
	refA := issueExternref(t, rtA, "A")
	sharedA, err := rtA.NewExternRefGlobal(refA, true)
	if err != nil {
		t.Fatalf("NewExternRefGlobal A: %v", err)
	}
	defer sharedA.Close()

	mutableExtern, err := rtB.Compile(importedReferenceGlobalModule(wasm.ExternRef, true))
	if err != nil {
		t.Fatalf("Compile externref importer: %v", err)
	}
	if _, err := rtB.Instantiate(context.Background(), mutableExtern, WithImports(Imports{"env.ref": sharedA})); err == nil || !strings.Contains(err.Error(), "incompatible reference store") {
		t.Fatalf("cross-runtime import error = %v", err)
	}
	wrongType := NewGlobalI64(0, true)
	defer wrongType.Close()
	if _, err := rtB.Instantiate(context.Background(), mutableExtern, WithImports(Imports{"env.ref": wrongType})); err == nil || !strings.Contains(err.Error(), "type i64") {
		t.Fatalf("wrong-type import error = %v", err)
	}
	immutable, err := rtB.NewExternRefGlobal(NullExternRef(), false)
	if err != nil {
		t.Fatalf("NewExternRefGlobal immutable: %v", err)
	}
	defer immutable.Close()
	if _, err := rtB.Instantiate(context.Background(), mutableExtern, WithImports(Imports{"env.ref": immutable})); err == nil || !strings.Contains(err.Error(), "mutability") {
		t.Fatalf("wrong-mutability import error = %v", err)
	}
	owned, err := rtB.NewExternRefGlobal(NullExternRef(), true)
	if err != nil {
		t.Fatalf("NewExternRefGlobal owned: %v", err)
	}
	defer owned.Close()
	owned.Type = ValFuncRef
	if _, err := rtB.Instantiate(context.Background(), mutableExtern, WithImports(Imports{"env.ref": owned})); err == nil || !strings.Contains(err.Error(), "public metadata") {
		t.Fatalf("mutated public metadata import error = %v", err)
	}
	owned.Type = ValExternRef
	foreign := issueExternref(t, rtA, "foreign")
	if _, err := rtB.NewExternRefGlobal(foreign, true); err == nil || !strings.Contains(err.Error(), "invalid externref token") {
		t.Fatalf("cross-store constructor error = %v", err)
	}
	forged := ValueOf(ValExternRef, ValueExternRef(foreign).Bits()^0x9e3779b97f4a7c15).ExternRef()
	if _, err := rtB.NewExternRefGlobal(forged, true); err == nil || !strings.Contains(err.Error(), "invalid externref token") {
		t.Fatalf("forged constructor error = %v", err)
	}

	privateCompiled, err := Compile(nil, nullableLocalExternrefGlobalsModule())
	if err != nil {
		t.Fatalf("Compile private producer: %v", err)
	}
	defer privateCompiled.Close()
	private, err := Instantiate(privateCompiled)
	if err != nil {
		t.Fatalf("Instantiate private producer: %v", err)
	}
	defer private.Close()
	privateRef := issueExternref(t, private, "private")
	if err := private.SetGlobalValue("mutable", ValueExternRef(privateRef)); err != nil {
		t.Fatalf("seed private global: %v", err)
	}
	privateGlobal, err := private.ExportedGlobalObject("mutable")
	if err != nil {
		t.Fatalf("Export private global: %v", err)
	}
	if _, err := rtB.Instantiate(context.Background(), mutableExtern, WithImports(Imports{"env.ref": privateGlobal})); err == nil || !strings.Contains(err.Error(), "incompatible reference store") {
		t.Fatalf("private-store import error = %v", err)
	}

	funcrefProducerMod, err := rtB.Compile(noTableRefFuncGlobalModule())
	if err != nil {
		t.Fatalf("Compile corrupted funcref producer: %v", err)
	}
	funcrefProducer, err := rtB.Instantiate(context.Background(), funcrefProducerMod)
	if err != nil {
		t.Fatalf("Instantiate corrupted funcref producer: %v", err)
	}
	defer funcrefProducer.Close()
	corrupted, err := funcrefProducer.ExportedGlobalObject("target_ref")
	if err != nil {
		t.Fatalf("Export corrupted funcref global: %v", err)
	}
	writeGlobalObject(corrupted, ValFuncRef, 1)
	funcrefImporter, err := rtB.Compile(importedGlobalGetInitializerModule(wasm.FuncRef))
	if err != nil {
		t.Fatalf("Compile funcref importer: %v", err)
	}
	if _, err := rtB.Instantiate(context.Background(), funcrefImporter, WithImports(Imports{"env.ref": corrupted})); err == nil || !strings.Contains(err.Error(), "invalid funcref descriptor") {
		t.Fatalf("corrupted descriptor import error = %v", err)
	}
}

func TestReferenceGlobalCloseOrderingAliasesAndStoreRoots(t *testing.T) {
	rt := NewRuntime()
	ref := issueExternref(t, rt, "rooted-by-global")
	shared, err := rt.NewExternRefGlobal(ref, true)
	if err != nil {
		t.Fatalf("NewExternRefGlobal: %v", err)
	}
	aliasMod, err := rt.Compile(duplicateImportedReferenceGlobalsModule(wasm.ExternRef, true))
	if err != nil {
		t.Fatalf("Compile alias importer: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), aliasMod, WithImports(Imports{"env.ref": shared}))
	if err != nil {
		t.Fatalf("Instantiate aliases: %v", err)
	}
	if got := shared.owner.importers; got != 1 {
		t.Fatalf("duplicate aliases attached %d importer roots, want 1", got)
	}
	if err := shared.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
		t.Fatalf("Close with importer error = %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	if value, ok := rt.ExternRefValue(ref); !ok || value != "rooted-by-global" {
		t.Fatalf("root after Runtime.Close = %#v, %v", value, ok)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("Instance.Close: %v", err)
	}
	if got := shared.owner.importers; got != 0 {
		t.Fatalf("importers after close = %d, want 0", got)
	}
	if value, ok := rt.ExternRefValue(ref); !ok || value != "rooted-by-global" {
		t.Fatalf("root before final Global.Close = %#v, %v", value, ok)
	}
	if err := shared.Close(); err != nil {
		t.Fatalf("final Global.Close: %v", err)
	}
	if _, ok := rt.ExternRefValue(ref); ok {
		t.Fatal("store retained root after Runtime, instance, and global close")
	}
}

func TestReferenceGlobalPersistenceAndFootprintsStayBounded(t *testing.T) {
	if !requireStandardGoTestRuntime(t) {
		return
	}
	if got := unsafe.Sizeof(Global{}); got != 40 {
		t.Fatalf("Global size = %d, want 40", got)
	}
	if got := unsafe.Sizeof(Instance{}); got != 864 {
		t.Fatalf("Instance size = %d, want 864", got)
	}
	if got := unsafe.Sizeof(Compiled{}); got != 632 {
		t.Fatalf("Compiled size = %d, want 632", got)
	}
	if got := unsafe.Sizeof(referenceStore{}); got != 88 {
		t.Fatalf("referenceStore size = %d, want 88", got)
	}
	c := &Compiled{
		GlobalImports: []GlobalImportDef{{Module: "env", Name: "ref", Type: ValExternRef}},
		Globals:       []GlobalDef{{Type: ValExternRef}},
	}
	_ = roundTripCompiled(t, c)
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "reference global metadata") {
		t.Fatalf("Capture error = %v", err)
	}
}

func TestRelease2ImportedReferenceGlobalSourceGuard(t *testing.T) {
	if !requireStandardGoTestRuntime(t) {
		return
	}
	raw, err := os.ReadFile(filepath.Clean("../../tests/spec-v2/test/core/linking.wast"))
	if err != nil {
		t.Skipf("Release 2 linking.wast unavailable: %v", err)
	}
	for _, text := range []string{
		`(module $Mref_ex`,
		`(global (export "g-const-func") funcref (ref.null func))`,
		`(global (export "g-var-func") (mut funcref) (ref.null func))`,
		`(global (export "g-const-extern") externref (ref.null extern))`,
		`(global (export "g-var-extern") (mut externref) (ref.null extern))`,
		`(module $Mref_im`,
		`(global (import "Mref_ex" "g-const-func") funcref)`,
		`(global (import "Mref_ex" "g-const-extern") externref)`,
		`(global (import "Mref_ex" "g-var-func") (mut funcref))`,
		`(global (import "Mref_ex" "g-var-extern") (mut externref))`,
		`(module (global (import "Mref_ex" "g-const-extern") funcref))`,
		`(module (global (import "Mref_ex" "g-const-func") externref))`,
		`(module (global (import "Mref_ex" "g-var-func") (mut externref)))`,
		`(module (global (import "Mref_ex" "g-var-extern") (mut funcref)))`,
	} {
		if !strings.Contains(string(raw), text) {
			t.Fatalf("linking.wast no longer contains %q", text)
		}
	}
}

func importedReferenceGlobalModule(refType wasm.ValType, mutable bool) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{refType}),
			wasmtest.FuncType([]wasm.ValType{refType}, []wasm.ValType{refType}),
		)),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", refType, mutable))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get", 0, 0),
			wasmtest.ExportEntry("set_and_get", 0, 1),
			wasmtest.ExportEntry("ref", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x24, 0x00, 0x23, 0x00, 0x0b}),
		)),
	)
}

func importedGlobalGetInitializerModule(refType wasm.ValType) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{refType}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", refType, false))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(refType, false, []byte{0x23, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get", 0, 0),
			wasmtest.ExportEntry("copy", 3, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x01, 0x0b}))),
	)
}

func duplicateImportedReferenceGlobalsModule(refType wasm.ValType, mutable bool) []byte {
	entry := wasmtest.GlobalImportEntry("env", "ref", refType, mutable)
	return wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(entry, entry)))
}

package wago

import (
	"context"
	"math/bits"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type externrefIssuer interface {
	NewExternRef(any) (ExternRef, error)
}

type externrefResolver interface {
	ExternRefValue(ExternRef) (any, bool)
}

func issueExternref(t *testing.T, owner any, value any) ExternRef {
	t.Helper()
	issuer, ok := owner.(externrefIssuer)
	if !ok {
		t.Fatalf("%T does not expose NewExternRef", owner)
	}
	ref, err := issuer.NewExternRef(value)
	if err != nil {
		t.Fatalf("NewExternRef: %v", err)
	}
	if ref.IsNull() {
		t.Fatal("NewExternRef returned null")
	}
	return ref
}

func resolveExternref(t *testing.T, owner any, ref ExternRef) any {
	t.Helper()
	resolver, ok := owner.(externrefResolver)
	if !ok {
		t.Fatalf("%T does not expose ExternRefValue", owner)
	}
	value, ok := resolver.ExternRefValue(ref)
	if !ok {
		t.Fatalf("ExternRefValue rejected non-null reference")
	}
	return value
}

func TestExternrefParamsResultsLocalsAndControlFlow(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(externrefControlModule())
	if err != nil {
		t.Fatalf("Compile externref control module: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate externref control module: %v", err)
	}
	defer in.Close()

	leftObject := &struct{ name string }{"left"}
	rightObject := &struct{ name string }{"right"}
	left := issueExternref(t, rt, leftObject)
	right := issueExternref(t, rt, rightObject)

	for _, tc := range []struct {
		name   string
		export string
		args   []Value
		want   ExternRef
	}{
		{name: "id", export: "id", args: []Value{ValueExternRef(left)}, want: left},
		{name: "typed_select_left", export: "select", args: []Value{ValueExternRef(left), ValueExternRef(right), ValueI32(1)}, want: left},
		{name: "typed_select_right", export: "select", args: []Value{ValueExternRef(left), ValueExternRef(right), ValueI32(0)}, want: right},
		{name: "branch", export: "branch", args: []Value{ValueI32(1), ValueExternRef(left)}, want: left},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := in.Call(context.Background(), tc.export, tc.args...)
			if err != nil {
				t.Fatalf("Call: %v", err)
			}
			if len(got) != 1 || got[0].Type() != ValExternRef || got[0].ExternRef() != tc.want {
				t.Fatalf("result = %v, want externref identity", got)
			}
		})
	}
	if got := resolveExternref(t, in, left); got != leftObject {
		t.Fatalf("resolved left object = %#v, want original pointer", got)
	}

	for _, name := range []string{"local_zero", "null", "block_null"} {
		got, err := in.Call(context.Background(), name)
		if err != nil {
			t.Fatalf("Call %s: %v", name, err)
		}
		if len(got) != 1 || got[0].Type() != ValExternRef || !got[0].ExternRef().IsNull() {
			t.Fatalf("Call %s = %v, want null externref", name, got)
		}
	}
	for _, tc := range []struct {
		name string
		arg  Value
		want int32
	}{
		{name: "null", arg: ValueExternRef(NullExternRef()), want: 1},
		{name: "non-null", arg: ValueExternRef(left), want: 0},
	} {
		got, err := in.Call(context.Background(), "is_null", tc.arg)
		if err != nil {
			t.Fatalf("is_null(%s): %v", tc.name, err)
		}
		if len(got) != 1 || got[0].I32() != tc.want {
			t.Fatalf("is_null(%s) = %v, want %d", tc.name, got, tc.want)
		}
	}
}

func TestExternrefHostImportRoundTripsObjects(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(externrefHostRoundTripModule())
	if err != nil {
		t.Fatalf("Compile externref host module: %v", err)
	}
	inputObject := &struct{ id int }{42}
	outputObject := &struct{ id int }{99}
	input := issueExternref(t, rt, inputObject)
	calls := 0
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{
		"env.echo": HostFunc(func(m HostModule, params, results []uint64) {
			calls++
			ref := ValueOf(ValExternRef, params[0]).ExternRef()
			if got := resolveExternref(t, m, ref); got != inputObject {
				t.Fatalf("host resolved %#v, want original input object", got)
			}
			output := issueExternref(t, m, outputObject)
			results[0] = ValueExternRef(output).Bits()
		}),
	}))
	if err != nil {
		t.Fatalf("Instantiate externref host module: %v", err)
	}
	defer in.Close()

	got, err := in.Call(context.Background(), "roundtrip", ValueExternRef(input))
	if err != nil {
		t.Fatalf("Call roundtrip: %v", err)
	}
	if calls != 1 || len(got) != 1 || got[0].ExternRef().IsNull() || got[0].ExternRef() == input {
		t.Fatalf("roundtrip = %v calls=%d, want new non-null externref and one host call", got, calls)
	}
	if resolved := resolveExternref(t, rt, got[0].ExternRef()); resolved != outputObject {
		t.Fatalf("host result resolved %#v, want output object", resolved)
	}
}

func TestExternrefCrossInstanceCallsRequireSameStore(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	producerMod, err := rt.Compile(externrefControlModule())
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	defer producer.Close()
	target, err := producer.ExportedFunc("id")
	if err != nil {
		t.Fatalf("Export id: %v", err)
	}
	consumerMod, err := rt.Compile(externrefHostRoundTripModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerMod, WithImports(Imports{"env.echo": target}))
	if err != nil {
		t.Fatalf("Instantiate same-store consumer: %v", err)
	}
	defer consumer.Close()
	ref := issueExternref(t, rt, "same-store")
	if got, err := consumer.Call(context.Background(), "roundtrip", ValueExternRef(ref)); err != nil || len(got) != 1 || got[0].ExternRef() != ref {
		t.Fatalf("same-store cross-instance roundtrip = %v, %v", got, err)
	}

	foreignRT := NewRuntime()
	defer foreignRT.Close()
	foreignMod, err := foreignRT.Compile(externrefHostRoundTripModule())
	if err != nil {
		t.Fatalf("Compile foreign consumer: %v", err)
	}
	if in, err := foreignRT.Instantiate(context.Background(), foreignMod, WithImports(Imports{"env.echo": target})); err == nil || !strings.Contains(err.Error(), "same reference store") {
		if in != nil {
			_ = in.Close()
		}
		t.Fatalf("cross-runtime externref import error = %v, want store rejection", err)
	}

	standaloneConsumer := MustCompile(externrefHostRoundTripModule())
	defer standaloneConsumer.Close()
	if in, err := Instantiate(standaloneConsumer, InstantiateOptions{Imports: Imports{"env.echo": target}}); err == nil || !strings.Contains(err.Error(), "same reference store") {
		if in != nil {
			_ = in.Close()
		}
		t.Fatalf("standalone externref import error = %v, want store rejection", err)
	}
}

func TestExternrefHostResultRejectsForgedTokenBeforeWasmReentry(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(externrefForgedHostResultModule())
	if err != nil {
		t.Fatalf("Compile forged-result module: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{
		"env.bad": HostFunc(func(_ HostModule, _ []uint64, results []uint64) {
			results[0] = 0xfeedfacecafebeef
		}),
	}))
	if err != nil {
		t.Fatalf("Instantiate forged-result module: %v", err)
	}
	defer in.Close()

	if got, err := in.Invoke("run"); err == nil || !strings.Contains(err.Error(), "invalid externref token") || got != nil {
		t.Fatalf("run = %v, %v; want forged host-result rejection", got, err)
	}
	if marker, err := in.Global("marker"); err != nil || AsI32(marker) != 0 {
		t.Fatalf("marker = %v, %v; wasm resumed after forged host result", marker, err)
	}
}

func TestExternrefRejectsForgedCrossRuntimeAndPrivateStoreTokensBeforeExecution(t *testing.T) {
	producerRT := NewRuntime()
	producer := issueExternref(t, producerRT, "runtime-a")
	defer producerRT.Close()

	consumerRT := NewRuntime()
	defer consumerRT.Close()
	mod, err := consumerRT.Compile(externrefIngressMarkerModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	consumer, err := consumerRT.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()

	for name, ref := range map[string]ExternRef{
		"cross-runtime": producer,
		"forged":        ValueOf(ValExternRef, ValueExternRef(producer).Bits()^0xa5a5a5a5a5a5a5a5).ExternRef(),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := consumer.Call(context.Background(), "sink", ValueExternRef(ref))
			if err == nil || !strings.Contains(err.Error(), "invalid externref token") || got != nil {
				t.Fatalf("sink = %v, %v; want invalid externref token", got, err)
			}
			if marker, markerErr := consumer.Global("marker"); markerErr != nil || AsI32(marker) != 0 {
				t.Fatalf("marker = %v, %v; native body ran before rejection", marker, markerErr)
			}
		})
	}

	privateA := instantiateFuncrefBoundaryTestModule(t, externrefIngressMarkerModule())
	defer privateA.Close()
	privateB := instantiateFuncrefBoundaryTestModule(t, externrefIngressMarkerModule())
	defer privateB.Close()
	privateRef := issueExternref(t, privateA, "private-a")
	if got, err := privateB.Call(context.Background(), "sink", ValueExternRef(privateRef)); err == nil || !strings.Contains(err.Error(), "invalid externref token") || got != nil {
		t.Fatalf("cross-private sink = %v, %v; want invalid externref token", got, err)
	}
}

func TestExternrefGenerationAndStoreTeardown(t *testing.T) {
	store := newReferenceStore(true)
	token, err := store.issueExternref("stale")
	if err != nil {
		t.Fatalf("issue externref: %v", err)
	}
	raw := bits.RotateLeft64(token, -17) ^ store.externKey
	index := uint32(raw)
	if index == 0 || int(index) > len(store.externrefs) {
		t.Fatalf("decoded index = %d, slots=%d", index, len(store.externrefs))
	}
	store.externrefs[index-1].generation++
	if _, ok := store.resolveExternref(token); ok {
		t.Fatal("stale generation resolved")
	}

	rt := NewRuntime()
	ref := issueExternref(t, rt, "released")
	if len(rt.refStore.externrefs) != 1 {
		t.Fatalf("runtime externref slots = %d, want 1", len(rt.refStore.externrefs))
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	if len(rt.refStore.externrefs) != 0 || rt.refStore.externKey != 0 {
		t.Fatalf("closed runtime retained externrefs: slots=%d key=%#x", len(rt.refStore.externrefs), rt.refStore.externKey)
	}
	if _, ok := rt.ExternRefValue(ref); ok {
		t.Fatal("released runtime token still resolved")
	}
}

func TestExternrefCodecCarriesStructureButNoStoreIdentity(t *testing.T) {
	c := compileExplicitArtifact(t, externrefControlModule())
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	var decoded Compiled
	if err := decoded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}

	producerRT := NewRuntime()
	foreign := issueExternref(t, producerRT, "foreign")
	defer producerRT.Close()
	consumerRT := NewRuntime()
	defer consumerRT.Close()
	in, err := instantiateCore(&decoded, InstantiateOptions{store: consumerRT.refStore})
	if err != nil {
		t.Fatalf("Instantiate decoded module: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("id", ValueExternRef(foreign).Bits()); err == nil || !strings.Contains(err.Error(), "invalid externref token") || got != nil {
		t.Fatalf("decoded module accepted foreign token: %v, %v", got, err)
	}
	if got, err := in.Invoke("id", 0); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("decoded module null round trip = %v, %v", got, err)
	}
}

func TestExternrefFeatureGateAndOfficialSourceSites(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	if _, err := Compile(cfg, externrefControlModule()); err == nil || !strings.Contains(err.Error(), "reference-types disabled") || !strings.Contains(err.Error(), "externref") {
		t.Fatalf("Compile with reference types disabled = %v, want externref feature gate", err)
	}

	sites := []struct {
		file string
		text string
	}{
		{"ref_null.wast", `(func (export "externref") (result externref) (ref.null extern))`},
		{"ref_is_null.wast", `(func $f2 (export "externref") (param $x externref) (result i32)`},
		{"select.wast", `(func (export "select-externref") (param externref externref i32) (result externref)`},
		{"br_table.wast", `(func (export "meet-externref") (param i32) (param externref) (result externref)`},
	}
	for _, site := range sites {
		raw, err := os.ReadFile(filepath.Join("../../tests/spec-v2/test/core", site.file))
		if err != nil {
			t.Skipf("Release 2 fixture unavailable: %v", err)
		}
		if !strings.Contains(string(raw), site.text) {
			t.Fatalf("%s no longer contains pinned externref site %q", site.file, site.text)
		}
	}
}

func externrefControlModule() []byte {
	externref := wasm.ExternRef
	returnExternref := wasmtest.FuncType(nil, []wasm.ValType{externref})
	returnI32 := wasmtest.FuncType([]wasm.ValType{externref}, []wasm.ValType{wasm.I32})
	returnExternrefIndex := byte(1)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{externref}, []wasm.ValType{externref}),
			returnExternref,
			returnI32,
			wasmtest.FuncType([]wasm.ValType{externref, externref, wasm.I32}, []wasm.ValType{externref}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, externref}, []wasm.ValType{externref}),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1),
			wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("id", 0, 0),
			wasmtest.ExportEntry("local_zero", 0, 1),
			wasmtest.ExportEntry("null", 0, 2),
			wasmtest.ExportEntry("block_null", 0, 3),
			wasmtest.ExportEntry("is_null", 0, 4),
			wasmtest.ExportEntry("select", 0, 5),
			wasmtest.ExportEntry("branch", 0, 6),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			codeWithLocalRun(1, wasm.MustEncodeValType(externref), []byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x6f, 0x0b}),
			wasmtest.Code([]byte{0x02, returnExternrefIndex, 0xd0, 0x6f, 0x0b, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1c, 0x01, 0x6f, 0x0b}),
			wasmtest.Code([]byte{0x02, 0x6f, 0x20, 0x01, 0x20, 0x00, 0x0e, 0x01, 0x00, 0x00, 0x0b, 0x0b}),
		)),
	)
}

func externrefHostRoundTripModule() []byte {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "echo", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roundtrip", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	)
}

func externrefForgedHostResultModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.ExternRef}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "bad", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("marker", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x10, 0x00, 0x1a, // call bad; drop
			0x41, 0x01, 0x24, 0x00, // marker = 1 (must not execute)
			0x0b,
		}))),
	)
}

func externrefIngressMarkerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("sink", 0, 0),
			wasmtest.ExportEntry("marker", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x01, 0x24, 0x00, 0x0b}))),
	)
}

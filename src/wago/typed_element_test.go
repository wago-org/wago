//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func typedElementCallRef(t *testing.T, in *Instance, name string, index int32) ExternRef {
	t.Helper()
	out, err := in.Call(context.Background(), name, ValueI32(index))
	if err != nil || len(out) != 1 || out[0].Type() != ValExternRef {
		t.Fatalf("%s(%d) = %v, %v; want one externref", name, index, out, err)
	}
	return out[0].ExternRef()
}

func TestTypedExternrefElementsExecuteAcrossModesAndIndexes(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(watToWasm(t, `(module
		(table $fun 1 funcref)
		(table $ext 4 externref)
		(elem (table $fun) (i32.const 0) func $dummy)
		(elem (table $ext) (i32.const 1) externref (ref.null extern) (ref.null extern))
		(elem $passive externref (ref.null extern) (ref.null extern))
		(elem $declared declare externref (ref.null extern))
		(func $dummy)
		(func (export "get") (param i32) (result externref)
			(table.get $ext (local.get 0)))
		(func (export "set") (param i32 externref)
			(table.set $ext (local.get 0) (local.get 1)))
		(func (export "init") (param i32 i32 i32)
			(table.init $ext $passive (local.get 0) (local.get 1) (local.get 2)))
		(func (export "init-declared") (param i32)
			(table.init $ext $declared (i32.const 0) (i32.const 0) (local.get 0)))
		(func (export "drop") (elem.drop $passive))
	)`))
	if err != nil {
		t.Fatalf("Compile typed externref elements: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate typed externref elements: %v", err)
	}
	defer in.Close()

	refA := issueExternref(t, rt, "typed-element-a")
	refB := issueExternref(t, rt, "typed-element-b")
	for i, ref := range []ExternRef{refA, refA, refB, refB} {
		if _, err := in.Call(context.Background(), "set", ValueI32(int32(i)), ValueExternRef(ref)); err != nil {
			t.Fatalf("seed table[%d]: %v", i, err)
		}
	}
	// Active initialization ran before the host seeds above. Re-instantiation
	// proves its exact nonzero destination without conflating table 0's funcrefs.
	fresh, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate fresh typed externref elements: %v", err)
	}
	defer fresh.Close()
	if got := typedElementCallRef(t, fresh, "get", 0); !got.IsNull() {
		t.Fatalf("fresh table[0] = %v, want default null", got)
	}
	for _, index := range []int32{1, 2} {
		if got := typedElementCallRef(t, fresh, "get", index); !got.IsNull() {
			t.Fatalf("fresh table[%d] = %v, want active null", index, got)
		}
	}

	if _, err := in.Call(context.Background(), "init", ValueI32(1), ValueI32(0), ValueI32(2)); err != nil {
		t.Fatalf("table.init externref: %v", err)
	}
	for _, index := range []int32{1, 2} {
		if got := typedElementCallRef(t, in, "get", index); !got.IsNull() {
			t.Fatalf("table[%d] after init = %v, want null", index, got)
		}
	}
	if got := typedElementCallRef(t, in, "get", 0); got != refA {
		t.Fatalf("table[0] after init = %v, want untouched %v", got, refA)
	}
	if got := typedElementCallRef(t, in, "get", 3); got != refB {
		t.Fatalf("table[3] after init = %v, want untouched %v", got, refB)
	}
	if _, err := in.Call(context.Background(), "init", ValueI32(4), ValueI32(2), ValueI32(0)); err != nil {
		t.Fatalf("zero-length table.init at boundaries: %v", err)
	}
	if _, err := in.Call(context.Background(), "init-declared", ValueI32(0)); err != nil {
		t.Fatalf("zero-length table.init from declarative segment: %v", err)
	}
	if _, err := in.Call(context.Background(), "init-declared", ValueI32(1)); err == nil {
		t.Fatal("nonzero table.init from declarative segment unexpectedly succeeded")
	}
	if _, err := in.Invoke("drop"); err != nil {
		t.Fatalf("elem.drop externref: %v", err)
	}
	if _, err := in.Invoke("drop"); err != nil {
		t.Fatalf("repeated elem.drop externref: %v", err)
	}
	if _, err := in.Call(context.Background(), "init", ValueI32(0), ValueI32(0), ValueI32(0)); err != nil {
		t.Fatalf("zero-length init after drop: %v", err)
	}
	if _, err := in.Call(context.Background(), "init", ValueI32(0), ValueI32(0), ValueI32(1)); err == nil {
		t.Fatal("nonzero init after drop unexpectedly succeeded")
	}
}

func TestExternrefTableCopyPreservesIdentityOverlapAndBounds(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(watToWasm(t, `(module
		(table $a 5 externref)
		(table $b 5 externref)
		(func (export "get-a") (param i32) (result externref) (table.get $a (local.get 0)))
		(func (export "get-b") (param i32) (result externref) (table.get $b (local.get 0)))
		(func (export "set-a") (param i32 externref) (table.set $a (local.get 0) (local.get 1)))
		(func (export "set-b") (param i32 externref) (table.set $b (local.get 0) (local.get 1)))
		(func (export "copy-a") (param i32 i32 i32)
			(table.copy $a $a (local.get 0) (local.get 1) (local.get 2)))
		(func (export "copy-b-from-a") (param i32 i32 i32)
			(table.copy $b $a (local.get 0) (local.get 1) (local.get 2)))
	)`))
	if err != nil {
		t.Fatalf("Compile externref table.copy: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate externref table.copy: %v", err)
	}
	defer in.Close()

	refs := []ExternRef{
		issueExternref(t, rt, "copy-0"),
		issueExternref(t, rt, "copy-1"),
		issueExternref(t, rt, "copy-2"),
		issueExternref(t, rt, "copy-3"),
		issueExternref(t, rt, "copy-4"),
	}
	for i, ref := range refs {
		if _, err := in.Call(context.Background(), "set-a", ValueI32(int32(i)), ValueExternRef(ref)); err != nil {
			t.Fatalf("set-a[%d]: %v", i, err)
		}
	}
	if _, err := in.Call(context.Background(), "copy-b-from-a", ValueI32(1), ValueI32(0), ValueI32(3)); err != nil {
		t.Fatalf("cross-table externref copy: %v", err)
	}
	for i, ref := range refs[:3] {
		if got := typedElementCallRef(t, in, "get-b", int32(i+1)); got != ref {
			t.Fatalf("b[%d] = %v, want identity %v", i+1, got, ref)
		}
	}
	if _, err := in.Call(context.Background(), "copy-a", ValueI32(1), ValueI32(0), ValueI32(4)); err != nil {
		t.Fatalf("overlap copy source before destination: %v", err)
	}
	want := []ExternRef{refs[0], refs[0], refs[1], refs[2], refs[3]}
	for i, ref := range want {
		if got := typedElementCallRef(t, in, "get-a", int32(i)); got != ref {
			t.Fatalf("a[%d] after backward overlap = %v, want %v", i, got, ref)
		}
	}
	if _, err := in.Call(context.Background(), "copy-a", ValueI32(0), ValueI32(2), ValueI32(3)); err != nil {
		t.Fatalf("overlap copy source after destination: %v", err)
	}
	want = []ExternRef{refs[1], refs[2], refs[3], refs[2], refs[3]}
	for i, ref := range want {
		if got := typedElementCallRef(t, in, "get-a", int32(i)); got != ref {
			t.Fatalf("a[%d] after forward overlap = %v, want %v", i, got, ref)
		}
	}
	if _, err := in.Call(context.Background(), "copy-b-from-a", ValueI32(5), ValueI32(5), ValueI32(0)); err != nil {
		t.Fatalf("zero-length copy at boundaries: %v", err)
	}
	before := typedElementCallRef(t, in, "get-b", 4)
	if _, err := in.Call(context.Background(), "copy-b-from-a", ValueI32(4), ValueI32(0), ValueI32(2)); err == nil {
		t.Fatal("out-of-bounds externref copy unexpectedly succeeded")
	}
	if got := typedElementCallRef(t, in, "get-b", 4); got != before {
		t.Fatalf("trapped copy mutated b[4]: got %v, want %v", got, before)
	}
}

func TestActiveExternrefElementsPreserveDeclarationOrderOnFailedInstantiation(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	exporterMod, err := rt.Compile(watToWasm(t, `(module
		(table $t (export "t") 2 2 externref)
		(func (export "get") (param i32) (result externref) (table.get $t (local.get 0)))
		(func (export "set") (param i32 externref) (table.set $t (local.get 0) (local.get 1)))
	)`))
	if err != nil {
		t.Fatalf("Compile externref exporter: %v", err)
	}
	exporter, err := rt.Instantiate(context.Background(), exporterMod)
	if err != nil {
		t.Fatalf("Instantiate externref exporter: %v", err)
	}
	defer exporter.Close()
	shared, err := exporter.ExportedTable("t")
	if err != nil {
		t.Fatalf("ExportedTable(t): %v", err)
	}
	refA := issueExternref(t, rt, "active-a")
	refB := issueExternref(t, rt, "active-b")
	for i, ref := range []ExternRef{refA, refB} {
		if _, err := exporter.Call(context.Background(), "set", ValueI32(int32(i)), ValueExternRef(ref)); err != nil {
			t.Fatalf("seed shared table[%d]: %v", i, err)
		}
	}

	consumerMod, err := rt.Compile(watToWasm(t, `(module
		(import "producer" "t" (table $t 2 2 externref))
		(elem (table $t) (i32.const 0) externref (ref.null extern))
		(elem (table $t) (i32.const 1) externref (ref.null extern) (ref.null extern))
	)`))
	if err != nil {
		t.Fatalf("Compile failing active externref consumer: %v", err)
	}
	if _, err := rt.Instantiate(context.Background(), consumerMod, WithImports(Imports{"producer.t": shared})); err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("Instantiate failing active externref consumer error = %v, want bounds trap", err)
	}
	if got := typedElementCallRef(t, exporter, "get", 0); !got.IsNull() {
		t.Fatalf("earlier active segment effect = %v, want persisted null", got)
	}
	if got := typedElementCallRef(t, exporter, "get", 1); got != refB {
		t.Fatalf("failing segment partially mutated table[1]: got %v, want %v", got, refB)
	}
	if exporter.resourceRefs != 0 { // externref handles are store-owned; failed consumers add no producer root
		t.Fatalf("externref failed-instantiation resource roots = %d, want zero", exporter.resourceRefs)
	}
}

func TestTypedElementMetadataStaysBoundedAndRoundTripsCodecV20(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	baseline, err := Compile(nil, watToWasm(t, `(module (table 3 3 externref))`))
	if err != nil {
		t.Fatalf("Compile externref table baseline: %v", err)
	}
	defer baseline.Close()
	active, err := Compile(nil, watToWasm(t, `(module
		(table 3 3 externref)
		(elem (i32.const 0) externref (ref.null extern) (ref.null extern) (ref.null extern))
	)`))
	if err != nil {
		t.Fatalf("Compile active externref element: %v", err)
	}
	defer active.Close()
	passive, err := Compile(nil, watToWasm(t, `(module
		(table 3 3 externref)
		(elem $e externref (ref.null extern) (ref.null extern) (ref.null extern))
		(func (i32.const 0) (i32.const 0) (i32.const 0) (table.init 0 $e))
	)`))
	if err != nil {
		t.Fatalf("Compile passive externref element: %v", err)
	}
	defer passive.Close()
	for name, compiled := range map[string]*Compiled{"baseline": baseline, "active": active, "passive": passive} {
		if err := compiled.validateArenaFootprint(); err != nil {
			t.Fatalf("%s footprint: %v", name, err)
		}
	}
	if got := active.instantiateArenaNeed - baseline.instantiateArenaNeed; got != 0 {
		t.Fatalf("active externref element arena delta = %d, want zero", got)
	}
	if got, want := passive.instantiateArenaNeed-baseline.instantiateArenaNeed, coreruntime.PassiveElemDescBytes+3*8; got != want {
		t.Fatalf("passive externref element arena delta = %d, want %d", got, want)
	}
	if passive.NeedsFuncRefDescs {
		t.Fatal("externref-only element metadata requested funcref descriptors")
	}
	_ = roundTripCompiled(t, active)
	if _, err := Capture(active, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tables") {
		t.Fatalf("Capture active externref element error = %v, want table snapshot rejection", err)
	}
	if unsafe.Sizeof(Compiled{}) != 632 || unsafe.Sizeof(Instance{}) != 776 || unsafe.Sizeof(Table{}) != 64 || unsafe.Sizeof(Global{}) != 40 || unsafe.Sizeof(referenceStore{}) != 88 {
		t.Fatalf("layout changed: Compiled=%d Instance=%d Table=%d Global=%d referenceStore=%d", unsafe.Sizeof(Compiled{}), unsafe.Sizeof(Instance{}), unsafe.Sizeof(Table{}), unsafe.Sizeof(Global{}), unsafe.Sizeof(referenceStore{}))
	}
}

func TestRelease2TypedElementCompileGapSourceGuard(t *testing.T) {
	sites := map[string][]string{
		"bulk.wast": {
			`(func (elem.drop 64))`,
			`(module (elem funcref (ref.func 0)) (func (elem.drop 0)))`,
		},
		"elem.wast": {
			`(import "exporter" "table" (table $t 2 externref))`,
			`(elem (i32.const 0) externref (ref.null extern))`,
		},
		"table.wast": {
			`(module (table 0 65536 funcref))`,
			`(module (table 0 0xffff_ffff funcref))`,
		},
	}
	for file, snippets := range sites {
		raw, err := os.ReadFile(filepath.Clean("../../tests/spec-v2/test/core/" + file))
		if err != nil {
			t.Skipf("Release 2 %s unavailable: %v", file, err)
		}
		for _, snippet := range snippets {
			if !strings.Contains(string(raw), snippet) {
				t.Fatalf("%s no longer contains typed-element closeout site %q", file, snippet)
			}
		}
	}
}

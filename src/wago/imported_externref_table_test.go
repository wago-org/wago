//go:build linux && (amd64 || riscv64) && !tinygo

package wago

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

type externrefTableFactory interface {
	NewExternRefTable(minSize, maxSize uint32) (*Table, error)
}

func requireExternrefTable(t *testing.T, rt *Runtime, minSize, maxSize uint32) *Table {
	t.Helper()
	factory, ok := any(rt).(externrefTableFactory)
	if !ok {
		t.Fatal("Runtime does not implement the explicit store-bound NewExternRefTable constructor")
	}
	table, err := factory.NewExternRefTable(minSize, maxSize)
	if err != nil {
		t.Fatalf("NewExternRefTable(%d, %d): %v", minSize, maxSize, err)
	}
	return table
}

func TestStoreBoundExternrefTableImportsShareExactTypeStoreAndAliases(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	shared := requireExternrefTable(t, rt, 2, 4)
	defer shared.Close()

	mod, err := rt.Compile(watToWasm(t, `(module
		(import "env" "t" (table $t 1 4 externref))
		(export "shared" (table $t))
		(func (export "get") (param i32) (result externref)
			(table.get $t (local.get 0)))
		(func (export "set") (param i32 externref)
			(table.set $t (local.get 0) (local.get 1)))
		(func (export "size") (result i32) (table.size $t))
		(func (export "grow") (param externref i32) (result i32)
			(table.grow $t (local.get 0) (local.get 1)))
		(func (export "fill") (param i32 externref i32)
			(table.fill $t (local.get 0) (local.get 1) (local.get 2)))
	)`))
	if err != nil {
		t.Fatalf("Compile imported externref table: %v", err)
	}
	first, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.t": shared}))
	if err != nil {
		t.Fatalf("Instantiate first importer: %v", err)
	}
	defer first.Close()
	second, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.t": shared}))
	if err != nil {
		t.Fatalf("Instantiate second importer: %v", err)
	}
	defer second.Close()

	if got := tableTestCallI32(t, first, "size"); got != 2 {
		t.Fatalf("initial shared size = %d, want 2", got)
	}
	refA := issueExternref(t, rt, "shared-a")
	refB := issueExternref(t, rt, "shared-b")
	if _, err := first.Call(context.Background(), "set", ValueI32(1), ValueExternRef(refA)); err != nil {
		t.Fatalf("first set: %v", err)
	}
	out, err := second.Call(context.Background(), "get", ValueI32(1))
	if err != nil || len(out) != 1 || out[0].ExternRef() != refA {
		t.Fatalf("second get after alias write = %v, %v; want %v", out, err, refA)
	}
	out, err = second.Call(context.Background(), "grow", ValueExternRef(refB), ValueI32(2))
	if err != nil || len(out) != 1 || out[0].I32() != 2 {
		t.Fatalf("grow shared table = %v, %v; want old size 2", out, err)
	}
	if got := tableTestCallI32(t, first, "size"); got != 4 {
		t.Fatalf("aliased size after grow = %d, want 4", got)
	}
	if _, err := first.Call(context.Background(), "fill", ValueI32(2), ValueExternRef(refA), ValueI32(2)); err != nil {
		t.Fatalf("fill shared table: %v", err)
	}
	for _, index := range []int32{2, 3} {
		out, err := second.Call(context.Background(), "get", ValueI32(index))
		if err != nil || len(out) != 1 || out[0].ExternRef() != refA {
			t.Fatalf("get(%d) after fill = %v, %v; want %v", index, out, err, refA)
		}
	}
	if reexported, err := first.ExportedTable("shared"); err != nil || reexported != shared {
		t.Fatalf("imported table re-export = %p, %v; want original %p", reexported, err, shared)
	}
}

func TestLocalExternrefTableExportReimportsOnlyWithinItsStore(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	exporterMod, err := rt.Compile(watToWasm(t, `(module
		(table $t (export "t") 1 3 externref)
		(func (export "get") (param i32) (result externref) (table.get $t (local.get 0)))
		(func (export "set") (param i32 externref) (table.set $t (local.get 0) (local.get 1)))
	)`))
	if err != nil {
		t.Fatalf("Compile local externref exporter: %v", err)
	}
	consumerMod, err := rt.Compile(watToWasm(t, `(module
		(import "producer" "t" (table $t 1 3 externref))
		(export "again" (table $t))
		(func (export "get") (param i32) (result externref) (table.get $t (local.get 0)))
		(func (export "set") (param i32 externref) (table.set $t (local.get 0) (local.get 1)))
	)`))
	if err != nil {
		t.Fatalf("Compile externref table consumer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), exporterMod)
	if err != nil {
		t.Fatalf("Instantiate local externref exporter: %v", err)
	}
	defer producer.Close()
	table, err := producer.ExportedTable("t")
	if err != nil {
		t.Fatalf("ExportedTable(t): %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerMod, WithImports(Imports{"producer.t": table}))
	if err != nil {
		t.Fatalf("same-store re-import: %v", err)
	}
	defer consumer.Close()
	ref := issueExternref(t, rt, "local-export")
	if _, err := consumer.Call(context.Background(), "set", ValueI32(0), ValueExternRef(ref)); err != nil {
		t.Fatalf("consumer set: %v", err)
	}
	out, err := producer.Call(context.Background(), "get", ValueI32(0))
	if err != nil || len(out) != 1 || out[0].ExternRef() != ref {
		t.Fatalf("producer get after consumer write = %v, %v; want %v", out, err, ref)
	}
	if again, err := consumer.ExportedTable("again"); err != nil || again != table {
		t.Fatalf("re-exported local table = %p, %v; want %p", again, err, table)
	}

	foreignRT := NewRuntime()
	defer foreignRT.Close()
	foreignMod, err := foreignRT.Compile(watToWasm(t, `(module (import "producer" "t" (table 1 3 externref)))`))
	if err != nil {
		t.Fatalf("Compile foreign consumer: %v", err)
	}
	if _, err := foreignRT.Instantiate(context.Background(), foreignMod, WithImports(Imports{"producer.t": table})); err == nil || !strings.Contains(err.Error(), "reference store") {
		t.Fatalf("cross-runtime import error = %v, want incompatible reference store", err)
	}

	privateCompiled, err := Compile(nil, watToWasm(t, `(module (import "producer" "t" (table 1 3 externref)))`))
	if err != nil {
		t.Fatalf("Compile private consumer: %v", err)
	}
	defer privateCompiled.Close()
	if _, err := Instantiate(privateCompiled, Imports{"producer.t": table}); err == nil || !strings.Contains(err.Error(), "reference store") {
		t.Fatalf("private-store import error = %v, want explicit compatible reference store", err)
	}
}

func TestStoreBoundExternrefTableRejectsTypeLimitsAndCloseOrder(t *testing.T) {
	rt := NewRuntime()
	shared := requireExternrefTable(t, rt, 1, 2)
	funcref, err := NewTable(1, 2)
	if err != nil {
		t.Fatalf("NewTable: %v", err)
	}
	defer funcref.Close()

	externMod, err := rt.Compile(watToWasm(t, `(module (import "env" "t" (table 1 2 externref)))`))
	if err != nil {
		t.Fatalf("Compile externref importer: %v", err)
	}
	funMod, err := rt.Compile(watToWasm(t, `(module (import "env" "t" (table 1 2 funcref)))`))
	if err != nil {
		t.Fatalf("Compile funcref importer: %v", err)
	}
	tooLargeMin, err := rt.Compile(watToWasm(t, `(module (import "env" "t" (table 2 2 externref)))`))
	if err != nil {
		t.Fatalf("Compile larger-min importer: %v", err)
	}
	tooSmallMax, err := rt.Compile(watToWasm(t, `(module (import "env" "t" (table 0 1 externref)))`))
	if err != nil {
		t.Fatalf("Compile smaller-max importer: %v", err)
	}
	for name, tc := range map[string]struct {
		mod   *Module
		table *Table
		want  string
	}{
		"funcref as externref": {externMod, funcref, "element type"},
		"externref as funcref": {funMod, shared, "element type"},
		"minimum":              {tooLargeMin, shared, "required minimum"},
		"maximum":              {tooSmallMax, shared, "required maximum"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := rt.Instantiate(context.Background(), tc.mod, WithImports(Imports{"env.t": tc.table})); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Instantiate error = %v, want %q", err, tc.want)
			}
		})
	}

	live, err := rt.Instantiate(context.Background(), externMod, WithImports(Imports{"env.t": shared}))
	if err != nil {
		t.Fatalf("Instantiate live importer: %v", err)
	}
	if err := shared.Close(); err == nil || !strings.Contains(err.Error(), "importer") {
		t.Fatalf("Close with live importer error = %v, want close-order rejection", err)
	}
	if got := shared.Size(); got != 1 {
		t.Fatalf("table size after rejected close = %d, want 1", got)
	}
	if err := live.Close(); err != nil {
		t.Fatalf("live importer Close: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime Close with live table: %v", err)
	}
	ref, err := rt.NewExternRef("table-root")
	if err == nil {
		t.Fatalf("NewExternRef after Runtime.Close = %v, want closed-store rejection", ref)
	}
	if err := shared.Close(); err != nil {
		t.Fatalf("table Close after consumer: %v", err)
	}
	if err := shared.Close(); err != nil {
		t.Fatalf("second table Close: %v", err)
	}
}

func TestStoreBoundExternrefTableKeepsRootsUntilRuntimeAndTableClose(t *testing.T) {
	rt := NewRuntime()
	shared := requireExternrefTable(t, rt, 1, 1)
	mod, err := rt.Compile(watToWasm(t, `(module
		(import "env" "t" (table 1 1 externref))
		(func (export "set") (param externref) (table.set 0 (i32.const 0) (local.get 0)))
	)`))
	if err != nil {
		t.Fatalf("Compile root fixture: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"env.t": shared}))
	if err != nil {
		t.Fatalf("Instantiate root fixture: %v", err)
	}
	ref := issueExternref(t, rt, "rooted-by-store-table")
	if _, err := in.Call(context.Background(), "set", ValueExternRef(ref)); err != nil {
		t.Fatalf("table.set root: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime Close: %v", err)
	}
	if value, ok := rt.ExternRefValue(ref); !ok || value != "rooted-by-store-table" {
		t.Fatalf("root after Runtime.Close with live instance/table = %#v, %v", value, ok)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("Instance Close: %v", err)
	}
	if value, ok := rt.ExternRefValue(ref); !ok || value != "rooted-by-store-table" {
		t.Fatalf("root after last instance but live table = %#v, %v", value, ok)
	}
	if err := shared.Close(); err != nil {
		t.Fatalf("Table Close: %v", err)
	}
	if _, ok := rt.ExternRefValue(ref); ok {
		t.Fatal("store retained externref after Runtime.Close, last instance close, and table close")
	}
}

func TestImportedExternrefTablePersistenceAndFootprintBoundaries(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	compiled, err := Compile(nil, watToWasm(t, `(module
		(import "env" "t" (table 1 2 externref))
		(export "t" (table 0))
	)`))
	if err != nil {
		t.Fatalf("Compile persistence fixture: %v", err)
	}
	defer compiled.Close()
	_ = roundTripCompiled(t, compiled)
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tables") {
		t.Fatalf("Capture error = %v, want table snapshot rejection", err)
	}
	if got := unsafe.Sizeof(Table{}); got != 64 {
		t.Fatalf("Table size = %d, want 64", got)
	}
	requireBoundedInstanceFootprint(t, unsafe.Sizeof(Instance{}))
	if got := unsafe.Sizeof(Compiled{}); got != 632 {
		t.Fatalf("Compiled size = %d, want 632", got)
	}
}

func TestRelease2ImportedExternrefTableSourceGuard(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean("../../tests/spec-v2/test/core/linking.wast"))
	if err != nil {
		t.Skipf("Release 2 linking.wast unavailable: %v", err)
	}
	for _, snippet := range []string{
		`(table $t2 (export "t-extern") 1 externref)`,
		`(table (import "Mtable_ex" "t-extern") 1 externref)`,
		`(module (table (import "Mtable_ex" "t-func") 1 externref))`,
		`(module (table (import "Mtable_ex" "t-extern") 1 funcref))`,
	} {
		if !strings.Contains(string(raw), snippet) {
			t.Fatalf("linking.wast no longer contains imported externref-table site %q", snippet)
		}
	}
}

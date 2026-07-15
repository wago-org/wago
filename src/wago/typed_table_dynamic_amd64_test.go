//go:build linux && amd64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

func typedTableDynamicModule(typeDefs [][]byte, targetType uint32, imported bool) []byte {
	refType := encodedNullableIndexedRef(targetType)
	i32 := []byte{0x7f}
	setType := uint32(len(typeDefs))
	callType := setType + 1
	growType := setType + 2
	fillType := setType + 3

	types := append([][]byte(nil), typeDefs...)
	types = append(types,
		encodedFuncType(nil, nil),
		encodedFuncType([][]byte{i32, i32}, [][]byte{i32}),
		encodedFuncType([][]byte{i32}, [][]byte{i32}),
		encodedFuncType([][]byte{i32, i32}, nil),
	)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
		entry = append(entry, 0x01)
		entry = append(entry, refType...)
		entry = append(entry, 0x01, 0x02, 0x06) // min=2, max=6
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(
		wasmtest.ULEB(targetType), wasmtest.ULEB(targetType),
		wasmtest.ULEB(setType), wasmtest.ULEB(setType),
		wasmtest.ULEB(callType), wasmtest.ULEB(growType),
		wasmtest.ULEB(fillType), wasmtest.ULEB(fillType), wasmtest.ULEB(fillType),
	)))
	if !imported {
		entry := append([]byte(nil), refType...)
		entry = append(entry, 0x01, 0x02, 0x06) // min=2, max=6
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec(entry)))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("table", 1, 0),
			wasmtest.ExportEntry("setA", 0, 2),
			wasmtest.ExportEntry("setB", 0, 3),
			wasmtest.ExportEntry("call", 0, 4),
			wasmtest.ExportEntry("growA", 0, 5),
			wasmtest.ExportEntry("fillB", 0, 6),
			wasmtest.ExportEntry("copy", 0, 7),
			wasmtest.ExportEntry("init", 0, 8),
		)),
	)
	passive := []byte{0x05}
	passive = append(passive, refType...)
	passive = append(passive, tableTestExprVec(tableTestRefFuncExpr(0), tableTestRefFuncExpr(1))...)
	sections = append(sections,
		wasmtest.Section(9, wasmtest.Vec(passive)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestI32Const(1), []byte{0x6a})),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestI32Const(10), []byte{0x6a})),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestRefFunc(0), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestI32Const(1), tableTestRefFunc(1), []byte{0x26, 0x00})),
			wasmtest.Code(tableTestBody(tableTestLocalGet(1), tableTestLocalGet(0), []byte{0x25, 0x00}, []byte{0x14}, wasmtest.ULEB(targetType))),
			wasmtest.Code(tableTestBody(tableTestRefFunc(0), tableTestLocalGet(0), tableTestBulk(15, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestRefFunc(1), tableTestLocalGet(1), tableTestBulk(17, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestLocalGet(1), tableTestI32Const(1), tableTestBulk(14, 0, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestLocalGet(1), tableTestI32Const(1), tableTestBulk(12, 0, 0))),
		)),
	)
	return wasmtest.Module(sections...)
}

func TestTypedFunctionReferenceDynamicTableLifecycle(t *testing.T) {
	tableTestForceExplicitBounds(t)
	producerModule := typedTableDynamicModule(
		[][]byte{encodedFuncType([][]byte{{0x7f}}, [][]byte{{0x7f}})}, 0, false,
	)
	consumerModule := typedTableDynamicModule(
		[][]byte{encodedFuncType(nil, nil), encodedFuncType([][]byte{{0x7f}}, [][]byte{{0x7f}})}, 1, true,
	)
	store := newReferenceStore(false)
	producer, err := instantiateCore(stagedTypedStorageCompile(t, producerModule), InstantiateOptions{store: store})
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	defer producer.Close()
	table, err := producer.ExportedTable("table")
	if err != nil {
		t.Fatalf("export typed table: %v", err)
	}
	consumer, err := instantiateCore(stagedTypedStorageCompile(t, consumerModule), InstantiateOptions{Imports: Imports{"env.table": table}, store: store})
	if err != nil {
		t.Fatalf("instantiate shifted-type consumer: %v", err)
	}

	alias, err := consumer.ExportedTable("table")
	if err != nil || alias != table {
		t.Fatalf("re-exported table alias = %p, %v; want original %p", alias, err, table)
	}
	if _, err := producer.Invoke("setA"); err != nil {
		t.Fatalf("producer setA: %v", err)
	}
	if _, err := producer.Invoke("setB"); err != nil {
		t.Fatalf("producer setB: %v", err)
	}
	if got := tableTestCallI32(t, consumer, "call", I32(0), I32(5)); got != 6 {
		t.Fatalf("call producer A = %d, want 6", got)
	}
	if got := tableTestCallI32(t, consumer, "call", I32(1), I32(5)); got != 15 {
		t.Fatalf("call producer B = %d, want 15", got)
	}
	if got := tableTestCallI32(t, consumer, "growA", I32(2)); got != 2 {
		t.Fatalf("growA old size = %d, want 2", got)
	}
	if got := tableTestCallI32(t, producer, "call", I32(3), I32(7)); got != 8 {
		t.Fatalf("grown producer A = %d, want 8", got)
	}
	if _, err := consumer.Invoke("fillB", I32(2), I32(2)); err != nil {
		t.Fatalf("fillB: %v", err)
	}
	if _, err := consumer.Invoke("copy", I32(0), I32(2)); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if got := tableTestCallI32(t, producer, "call", I32(0), I32(1)); got != 11 {
		t.Fatalf("copied consumer B = %d, want 11", got)
	}
	if _, err := consumer.Invoke("init", I32(1), I32(0)); err != nil {
		t.Fatalf("table.init: %v", err)
	}
	if got := tableTestCallI32(t, producer, "call", I32(1), I32(4)); got != 5 {
		t.Fatalf("initialized consumer A = %d, want 5", got)
	}

	// A trapping write must leave both the descriptor and its producer root intact.
	if _, err := consumer.Invoke("fillB", I32(6), I32(1)); err == nil {
		t.Fatal("out-of-bounds typed table.fill unexpectedly succeeded")
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer Close: %v", err)
	}
	if !consumer.hasResourceRoots() {
		t.Fatal("closed consumer not retained by live typed table descriptors")
	}
	if got := tableTestCallI32(t, producer, "call", I32(0), I32(2)); got != 12 {
		t.Fatalf("consumer descriptor after close = %d, want 12", got)
	}

	// Overwrite every consumer descriptor from the local table owner. The final
	// overwrite must prune the closed consumer even though the writer is not an
	// importer of its own exported table.
	if _, err := producer.Invoke("fillB", I32(0), I32(4)); err != nil {
		t.Fatalf("producer overwrite: %v", err)
	}
	if consumer.hasResourceRoots() || consumer.hasPhysicalResources() {
		t.Fatal("local typed table owner overwrite did not release closed consumer")
	}

	// Public admission remains closed even though the staged end-to-end path runs.
	if _, err := Compile(nil, producerModule); err == nil {
		t.Fatal("public typed-reference table compile unexpectedly succeeded")
	}
}

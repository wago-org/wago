package wago

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

// memoryModule builds a module declaring (and exporting) a memory of minP..maxP
// pages. Exporting it keeps MemMaxPages at maxP (an unexported, non-growing
// memory is pinned to its minimum).
func memoryModule(t *testing.T, minP, maxP int) *Module {
	t.Helper()
	memType := append([]byte{0x01}, wasmtest.ULEB(uint32(minP))...) // flags 0x01: has max
	memType = append(memType, wasmtest.ULEB(uint32(maxP))...)
	mod := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec(memType)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
	)
	m, err := NewRuntime().Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return m
}

func TestPolicyCapabilityAllowDeny(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil { // provides env.f, requires CapMetricsWrite
		t.Fatalf("use: %v", err)
	}
	mod := callsEnvF(t, rt)

	// Allowed list omitting the required capability → denied.
	_, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{AllowedCapabilities: []Capability{CapTimerRead}}))
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("instantiate with disallowed cap = %v, want ErrPermissionDenied", err)
	}

	// Explicit deny → denied even with a permissive allow-list.
	_, err = rt.Instantiate(context.Background(), mod, WithPolicy(Policy{DeniedCapabilities: []Capability{CapMetricsWrite}}))
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("instantiate with denied cap = %v, want ErrPermissionDenied", err)
	}

	// Allowed list including the capability → permitted.
	in, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{AllowedCapabilities: []Capability{CapMetricsWrite}}))
	if err != nil {
		t.Fatalf("instantiate with allowed cap: %v", err)
	}
	in.Close()

	// Zero policy is permissive.
	in, err = rt.Instantiate(context.Background(), mod, WithPolicy(Policy{}))
	if err != nil {
		t.Fatalf("instantiate with zero policy: %v", err)
	}
	in.Close()
}

func TestPolicyRejectsUnenforcedInvokeDuration(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{MaxInvokeDuration: time.Millisecond})); !errors.Is(err, ErrUnsupported) || errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("instantiate with deprecated invoke duration = %v, want ErrUnsupported only", err)
	}
}

func TestPolicyMemoryLimit(t *testing.T) {
	rt := NewRuntime()
	mod := memoryModule(t, 2, 4) // min 2 pages, max 4 pages -> 256 KiB max
	mod, err := rt.Module(mod.Compiled())
	if err != nil {
		t.Fatal(err)
	}
	// 128 KiB limit is below the module's 256 KiB max → denied.
	if _, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{MaxMemoryBytes: 128 << 10})); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("instantiate over memory limit = %v, want ErrPermissionDenied", err)
	}
	// 256 KiB limit fits.
	in, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{MaxMemoryBytes: 256 << 10}))
	if err != nil {
		t.Fatalf("instantiate within memory limit: %v", err)
	}
	in.Close()
}

func TestPolicyMemoryCountLimit(t *testing.T) {
	mod := &Module{c: &Compiled{memoryDir: &compiledMemoryDirectory{defs: []memoryDef{{}, {}}}}}
	if err := applyPolicy(mod, Policy{MaxMemories: 2}); err != nil {
		t.Fatalf("exact memory count policy: %v", err)
	}
	if err := applyPolicy(mod, Policy{MaxMemories: 1}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("memory count policy error = %v, want ErrPermissionDenied", err)
	}
}

func TestDeclaredMemoryMaxBytesChargesFullNoMaxMemory32Reservation(t *testing.T) {
	c := &Compiled{HasMemory: true, MemMinPages: 1}
	got, err := c.declaredMemoryMaxBytes()
	if err != nil {
		t.Fatalf("declaredMemoryMaxBytes: %v", err)
	}
	if want := uint64(1) << 32; got != want {
		t.Fatalf("no-max memory32 reservation = %d bytes, want %d", got, want)
	}
}

func TestPolicyChecksEveryLocalTable(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	wasm := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(
		[]byte{0x70, 0x00, 0x01},
		[]byte{0x70, 0x00, 0x03},
	)))
	mod, err := rt.Compile(wasm)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{MaxTableEntries: 2})); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("instantiate with oversized table 1 = %v, want ErrPermissionDenied", err)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{MaxTableEntries: 3}))
	if err != nil {
		t.Fatalf("instantiate within table limit: %v", err)
	}
	in.Close()
}

func TestPolicyTableLimitChecksAllocatedCapacity(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	declaredWasm := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(
		append([]byte{0x70, 0x01, 0x01}, wasmtest.ULEB(3)...),
	)))
	mod, err := rt.Compile(declaredWasm)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{MaxTableEntries: 2})); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("instantiate with maximum above table policy = %v, want ErrPermissionDenied", err)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{MaxTableEntries: 3}))
	if err != nil {
		t.Fatalf("instantiate at declared table maximum: %v", err)
	}
	in.Close()

	unboundedWasm := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		tableTestFuncSection(0),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(
			tableTestRefNullFunc(), tableTestI32Const(1), tableTestBulk(15, 0),
		)))),
	)
	unbounded, err := rt.Compile(unboundedWasm)
	if err != nil {
		t.Fatalf("compile no-max table: %v", err)
	}
	if err := applyPolicy(unbounded, Policy{MaxTableEntries: 1023}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("no-max table with 1024-entry runtime capacity = %v, want ErrPermissionDenied", err)
	}
}

func TestPolicyChecksImportedAndLocalTablesIndependently(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	compile := func(importMin, localMin uint32) *Module {
		t.Helper()
		wasm := wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", importMin, importMin))),
			wasmtest.Section(4, wasmtest.Vec(append([]byte{0x70, 0x00}, wasmtest.ULEB(localMin)...))),
		)
		mod, err := rt.Compile(wasm)
		if err != nil {
			t.Fatalf("compile imported+local tables: %v", err)
		}
		return mod
	}

	localTooLarge := compile(1, 3)
	if _, err := rt.Instantiate(context.Background(), localTooLarge, WithPolicy(Policy{MaxTableEntries: 2})); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("instantiate with oversized local table = %v, want ErrPermissionDenied", err)
	}
	importTooLarge := compile(3, 1)
	if _, err := rt.Instantiate(context.Background(), importTooLarge, WithPolicy(Policy{MaxTableEntries: 2})); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("instantiate with oversized imported table minimum = %v, want ErrPermissionDenied", err)
	}
}

func TestPolicyTableLimitChecksResolvedImportCapacity(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	producerMod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))),
	))
	if err != nil {
		t.Fatalf("compile provider: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatalf("instantiate provider: %v", err)
	}
	defer producer.Close()
	table, err := producer.ExportedTable("table")
	if err != nil {
		t.Fatalf("export provider table: %v", err)
	}
	if capacity, ok := table.runtimeCapacity(); !ok || capacity != 1024 {
		t.Fatalf("provider table capacity = %d, %v; want 1024, true", capacity, ok)
	}

	consumerMod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 0))),
	))
	if err != nil {
		t.Fatalf("compile consumer: %v", err)
	}
	_, err = rt.Instantiate(context.Background(), consumerMod,
		WithImports(Imports{"env.table": table}),
		WithPolicy(Policy{MaxTableEntries: 2}),
	)
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("instantiate with oversized resolved table = %v, want ErrPermissionDenied", err)
	}
}

func TestPolicyChecksMultipleImportedAndLocalTablesIndependently(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	compile := func(firstMin, secondMin, localMin uint32) *Module {
		t.Helper()
		wasm := wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(
				tableTestImportTable("env", "first", firstMin, firstMin),
				tableTestImportTable("env", "second", secondMin, secondMin),
			)),
			wasmtest.Section(4, wasmtest.Vec(append([]byte{0x70, 0x00}, wasmtest.ULEB(localMin)...))),
		)
		mod, err := rt.Compile(wasm)
		if err != nil {
			t.Fatalf("compile multiple imported tables: %v", err)
		}
		return mod
	}

	for _, tc := range []struct {
		name                          string
		firstMin, secondMin, localMin uint32
	}{
		{name: "first import", firstMin: 3, secondMin: 1, localMin: 1},
		{name: "second import", firstMin: 1, secondMin: 3, localMin: 1},
		{name: "local", firstMin: 1, secondMin: 1, localMin: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod := compile(tc.firstMin, tc.secondMin, tc.localMin)
			if _, err := rt.Instantiate(context.Background(), mod, WithPolicy(Policy{MaxTableEntries: 2})); !errors.Is(err, ErrPermissionDenied) {
				t.Fatalf("instantiate with oversized %s table = %v, want ErrPermissionDenied", tc.name, err)
			}
		})
	}
}

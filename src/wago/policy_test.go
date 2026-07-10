package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
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

func TestPolicyMemoryLimit(t *testing.T) {
	rt := NewRuntime()
	mod := memoryModule(t, 2, 4) // min 2 pages, max 4 pages -> 256 KiB max
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

func TestPolicyOnClass(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(tripleExt{}); err != nil {
		t.Fatalf("use: %v", err)
	}
	mod := callsEnvF(t, rt)
	_, err := rt.Class(mod, ClassOptions{
		Pool:   PoolOptions{MaxInstances: 1},
		Policy: Policy{AllowedCapabilities: []Capability{CapTimerRead}},
	})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("class with disallowed cap = %v, want ErrPermissionDenied", err)
	}
}

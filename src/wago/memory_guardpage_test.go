//go:build linux && amd64 && wago_guardpage

package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestImportedMemoryGuardPage drives a module importing host memory under
// signals-based (guard-page) bounds checks: in-bounds store/load work against the
// guard-page-backed host memory, and an out-of-range access faults into a wasm
// trap via the signal handler rather than an inline check.
func TestImportedMemoryGuardPage(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	c, err := Compile(cfg, importMemModule())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if c.boundsMode != BoundsChecksSignalsBased {
		t.Fatal("module did not record signals-based mode")
	}
	mem, err := NewMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	if !mem.guarded {
		t.Fatal("NewMemory should be guard-page backed in a wago_guardpage build")
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.mem": mem}})
	if err != nil {
		t.Fatalf("instantiate imported memory under guard-page mode: %v", err)
	}
	defer in.Close()

	// wasm write -> host observes.
	if _, err := in.Invoke("store", I32(8), I32(0xCAFE)); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(mem.Bytes()[8:]); got != 0xCAFE {
		t.Fatalf("host sees mem[8] = %#x, want 0xCAFE", got)
	}
	// host write -> wasm observes.
	binary.LittleEndian.PutUint32(mem.Bytes()[16:], 0x1234)
	if r, err := in.Invoke("load", I32(16)); err != nil || AsI32(r[0]) != 0x1234 {
		t.Fatalf("wasm load = %#x err=%v, want 0x1234", AsI32(r[0]), err)
	}
	// Out-of-range load (memory is 1 page = 64 KiB) traps via the guard page.
	if _, err := in.Invoke("load", I32(1<<20)); err == nil {
		t.Fatal("out-of-bounds load did not trap")
	}
}

// TestImportedMemoryGuardPageCrossInstance shares a guard-page memory between two
// signals-based instances: A owns it, B imports A's export, and writes are
// mutually visible through the one guarded reservation.
func TestImportedMemoryGuardPageCrossInstance(t *testing.T) {
	// A: memory 1; load8_u(a)->i32; store8(a,v).
	modA := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})), // 1 memory, min 1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("load", 0, 0),
			wasmtest.ExportEntry("store", 0, 1),
			wasmtest.ExportEntry("mem", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b}),             // load8_u
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x3a, 0x00, 0x00, 0x0b}), // store8
		)),
	)
	inA, err := Instantiate(MustCompile(modA), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer inA.Close()
	if !inA.memory.guarded {
		t.Fatal("A's memory should be guard-page backed under the guard tag")
	}
	memImport, err := inA.ExportedMemory("mem")
	if err != nil {
		t.Fatalf("export mem: %v", err)
	}

	// B imports env.mem; write8(a,v); load8_u(a)->i32.
	memEntry := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
	memEntry = append(memEntry, 0x02, 0x00, 0x01) // ExternMem, min 1
	modB := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("write", 0, 0),
			wasmtest.ExportEntry("load", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x3a, 0x00, 0x00, 0x0b}), // store8
			wasmtest.Code([]byte{0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b}),             // load8_u
		)),
	)
	inB, err := Instantiate(MustCompile(modB), InstantiateOptions{Imports: Imports{"env.mem": memImport}})
	if err != nil {
		t.Fatalf("instantiate B on shared guard-page memory: %v", err)
	}
	defer inB.Close()

	// B writes byte 11 = 99 -> A observes.
	if _, err := inB.Invoke("write", I32(11), I32(99)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inA.Invoke("load", I32(11)); AsI32(r[0]) != 99 {
		t.Fatalf("A.load(11) = %d, want 99 (B's write)", AsI32(r[0]))
	}
	// A writes byte 20 = 55 -> B observes.
	if _, err := inA.Invoke("store", I32(20), I32(55)); err != nil {
		t.Fatal(err)
	}
	if r, _ := inB.Invoke("load", I32(20)); AsI32(r[0]) != 55 {
		t.Fatalf("B.load(20) = %d, want 55 (A's write)", AsI32(r[0]))
	}
}

// TestImportedMemoryGuardPageRejectsPlainMemory checks the guard gate: a
// signals-based module may not import a memory that is not guard-page backed. An
// explicit-bounds instance's memory is a plain mapping, so importing it into a
// signals-based module must be refused rather than silently eliding checks against
// an unguarded region.
func TestImportedMemoryGuardPageRejectsPlainMemory(t *testing.T) {
	// Owner compiled with explicit bounds -> its owned memory is not guarded.
	explicitCfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	ownerMod := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})), // 1 memory, min 1
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("mem", 2, 0))),
	)
	owner, err := Compile(explicitCfg, ownerMod)
	if err != nil {
		t.Fatalf("compile owner: %v", err)
	}
	inOwner, err := Instantiate(owner, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate owner: %v", err)
	}
	defer inOwner.Close()
	if inOwner.memory.guarded {
		t.Fatal("explicit-bounds owner memory should not be guarded")
	}
	memImport, err := inOwner.ExportedMemory("mem")
	if err != nil {
		t.Fatal(err)
	}

	// Importer compiled signals-based: it must reject the unguarded memory.
	sigCfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	importer, err := Compile(sigCfg, importMemModule())
	if err != nil {
		t.Fatalf("compile importer: %v", err)
	}
	if _, err := Instantiate(importer, InstantiateOptions{Imports: Imports{"env.mem": memImport}}); err == nil {
		t.Fatal("signals-based module importing an unguarded memory should be rejected")
	}
}

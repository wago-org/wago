//go:build linux && amd64 && wago_guardpage && !tinygo

package wago

import (
	"errors"
	"syscall"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func guardCommitFailureModule() []byte {
	grow := []byte{0x41, 0x01, 0x40, 0x00, 0x0b} // memory.grow 0 by one page
	load := append([]byte{0x41}, wasmtest.SLEB32(65536)...)
	load = append(load, 0x2d, 0x00, 0x00, 0x0b) // i32.load8_u at the grown page
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})), // memory 1 2
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("load", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(grow), wasmtest.Code(load))),
	)
}

func TestSignalGuardCommitFailureTrapsInsteadOfRefaulting(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := cfg.Compile(guardCommitFailureModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	if got, err := in.Invoke("grow"); err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("memory.grow = %v, %v; want [1]", got, err)
	}

	// Punch a hole in the still-logically-valid grown page. The next access
	// reaches the signal handler, but mprotect cannot recreate an unmapped page
	// and must become a deterministic Wasm trap rather than an infinite refault.
	const (
		hostPageBytes   = uintptr(4096)
		wasmMemoryBytes = uintptr(65536)
	)
	grownPage := in.jm.LinMemBase() + wasmMemoryBytes
	if _, _, errno := syscall.Syscall(syscall.SYS_MUNMAP, grownPage, hostPageBytes, 0); errno != 0 {
		t.Fatalf("munmap grown guard page: %v", errno)
	}

	_, err = in.Invoke("load")
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemCouldNotExtend {
		t.Fatalf("load through unmapped grown page = %v; want %s", err, coreruntime.TrapLinMemCouldNotExtend)
	}
}

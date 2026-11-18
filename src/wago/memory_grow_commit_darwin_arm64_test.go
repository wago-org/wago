//go:build darwin && arm64 && wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGuardedMemoryGrowCommitIsFunctionLocal(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0),
			wasmtest.ExportEntry("plain", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0x40, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x0b}),
		)),
	)
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := Compile(cfg, mod)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !guardedMemoryGrowEntry(compiled.InternalEntry[0]) || !guardedMemoryGrowEntry(compiled.InternalEntry[1]) || guardedMemoryGrowEntry(compiled.InternalEntry[2]) {
		t.Fatalf("guarded grow facts = %t/%t/%t, want true/true/false", guardedMemoryGrowEntry(compiled.InternalEntry[0]), guardedMemoryGrowEntry(compiled.InternalEntry[1]), guardedMemoryGrowEntry(compiled.InternalEntry[2]))
	}

	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	plain, err := in.WasmFunc("plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Invoke(); err != nil {
		t.Fatal(err)
	}
	if got := in.guardCommitPages; got != 2 {
		t.Fatalf("plain call changed guarded commit watermark to %d, want 2", got)
	}
	grow, err := in.WasmFunc("grow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := grow.Invoke(); err != nil {
		t.Fatal(err)
	}
	if got := in.guardCommitPages; got != 3 {
		t.Fatalf("grow call guarded commit watermark = %d, want 3", got)
	}
}

func TestGuardedMemoryGrowCommitAfterStart(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0x40, 0x00, 0x1a, 0x0b}),
		)),
	)
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	compiled, err := Compile(cfg, mod)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := in.guardCommitPages; got != 3 {
		t.Fatalf("start grow guarded commit watermark = %d, want 3", got)
	}
}

func TestGuardedMemoryGrowPreparedHostSession(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		t.Run(fmt.Sprintf("dynamic%t", dynamic), func(t *testing.T) {
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x10, 0, 0x0b}))),
			)
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased), mod)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			imports := NewImports()
			imports.HostFunc("env", "f", func(v int32) int32 { return v })
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("grow")
			if err != nil {
				t.Fatal(err)
			}
			s, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if !s.state.host || !fn.guardedMemoryGrow {
				t.Fatal("missing retained host lease or guarded growth bookkeeping")
			}
			var got []uint64
			if dynamic {
				got, err = s.invokeArgs([]uint64{1})
			} else {
				got, err = s.Invoke1(1)
			}
			if err != nil || len(got) != 1 || got[0] != 1 {
				t.Fatalf("grow: %v, %v", got, err)
			}
			if in.guardCommitPages != 3 {
				t.Fatalf("commit watermark=%d, want 3", in.guardCommitPages)
			}
			// Failure to grow must leave the committed view unchanged.
			got, err = s.Invoke1(1)
			if err != nil || len(got) != 1 || uint32(got[0]) != ^uint32(0) || in.guardCommitPages != 3 {
				t.Fatalf("failed grow: %v, %v pages=%d", got, err, in.guardCommitPages)
			}
		})
	}
}

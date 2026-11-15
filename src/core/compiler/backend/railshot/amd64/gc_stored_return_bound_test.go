//go:build amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Alternating zero-count declarations survive the production byte-backed
// decoder. They add declaration work without adding locals or expression bytes.
func gcStoredZeroRunModule(t *testing.T, runs, callees int) *wasm.Module {
	t.Helper()
	var funcs, bodies [][]byte
	for i := 0; i < callees; i++ {
		body := wasmtest.ULEB(uint32(runs))
		for j := 0; j < runs; j++ {
			body = append(body, 0, byte(0x7f-j%2))
		}
		body = append(body, 0xd0, 0x6d, 0x0b)
		funcs = append(funcs, []byte{0})
		bodies = append(bodies, append(wasmtest.ULEB(uint32(len(body))), body...))
	}
	caller := []byte{0}
	for round := 0; round < 2; round++ {
		for i := 0; i < callees; i++ {
			caller = append(caller, 0x10)
			caller = append(caller, wasmtest.ULEB(uint32(i))...)
			caller = append(caller, 0x1a)
		}
	}
	caller = append(caller, 0xd0, 0x6d, 0x0b)
	funcs = append(funcs, []byte{0})
	bodies = append(bodies, append(wasmtest.ULEB(uint32(len(caller))), caller...))
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x6d})),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
	dm, err := wasm.DecodeModuleByteBacked(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateDecodedByteBackedModule(dm); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < callees; i++ {
		c := dm.Module.Code[i]
		if len(c.Locals.Runs) != runs || len(c.BodyBytes) != 3 {
			t.Fatalf("callee%d retained runs=%d body=%d, want%d/3", i, len(c.Locals.Runs), len(c.BodyBytes), runs)
		}
		for _, run := range c.Locals.Runs {
			if run.Count != 0 {
				t.Fatal("fixture introduced a live local")
			}
		}
	}
	return dm.Module
}

func TestGCStoredReturnLocalRunBound(t *testing.T) {
	for _, runs := range []int{0, 2, 32, 33, 64} {
		t.Run(fmt.Sprintf("runs%d", runs), func(t *testing.T) {
			f := fn{m: gcStoredZeroRunModule(t, runs, 1)}
			budget := 1024
			got := f.proveStoredGCReturn(0, 0, &budget)
			want := runs <= 32
			if got != want {
				t.Fatalf("zero-count declaration proof admitted=%t, want%t for%d runs; budget=%d", got, want, runs, budget)
			}
			if !want && budget != 1024 {
				t.Fatalf("oversized declarations reached expression scan: budget=%d", budget)
			}
		})
	}
}

func TestGCStoredReturnLocalRunBoundAcrossCacheEviction(t *testing.T) {
	f := fn{m: gcStoredZeroRunModule(t, 64, 9), gcStructHelpers: true, policy: currentCodegenPolicy()}
	if !f.gcLocalTypeFactsEnabled() {
		t.Fatal("fixture requires enabled value facts")
	}
	for call := 0; call < 18; call++ {
		callee := uint32(call % 9)
		if f.provedStoredGCCallResult(callee) {
			t.Errorf("call%d admitted64 zero-count declarations in callee%d", call, callee)
		}
	}
}

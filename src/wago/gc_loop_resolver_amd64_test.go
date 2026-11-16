//go:build amd64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcLoopResolverModule(prefix []byte) []byte {
	// Parameters: count. Locals: array, index, total.
	body := []byte{2, 1, 0x63, 0, 2, 0x7f, 0x23, 0, 0x21, 1,
		0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40}
	body = append(body, prefix...)
	body = append(body, 0x20, 3, 0x20, 1, 0x20, 2, 0xfb, 11, 0, 0x6a, 0x21, 3,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x0b, 0x20, 3, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("sum", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x20, 0, 0xfb, 6, 0, 0x24, 0, 0x0b}), append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCLoopResolverExecution(t *testing.T) {
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLoopResolverModule(nil))
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		in, e := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true, CollectEveryAlloc: true}})
		if e != nil {
			t.Fatal(e)
		}
		defer in.Close()
		// An enclosing zero-iteration guard must not resolve a null array.
		if got, e := in.Invoke("sum", 0); e != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("zero null: %v %v", got, e)
		}
		if _, e := in.Invoke("sum", 1); e == nil {
			t.Fatal("null loop did not trap")
		}
		for _, length := range []uint64{0, 1, 2, 31, 32, 127, 4096} {
			if _, e := in.Invoke("init", length); e != nil {
				t.Fatal(e)
			}
			for _, n := range []uint64{0, length} {
				if got, e := in.Invoke("sum", n); e != nil || len(got) != 1 || got[0] != 7*n {
					t.Fatalf("length=%d n=%d: %v %v", length, n, got, e)
				}
			}
			if _, e := in.Invoke("sum", length+1); e == nil {
				t.Fatalf("length=%d missing bounds trap", length)
			}
		}
	}
}

func TestGCLoopResolverRejectsEarlierTrap(t *testing.T) {
	// Division by zero precedes the first array read; null resolution cannot move
	// ahead of it. Both the guarded zero path and ordinary trap order remain.
	prefix := []byte{0x41, 1, 0x41, 0, 0x6e, 0x1a}
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLoopResolverModule(prefix))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	in, e := Instantiate(c, InstantiateOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	if _, e := in.Invoke("sum", 0); e != nil {
		t.Fatal(e)
	}
	_, e = in.Invoke("sum", 1)
	if e == nil {
		t.Fatal("missing division trap")
	}
	// Compare with an initialized array, ensuring the earlier trap is unchanged.
	first := e.Error()
	if _, e := in.Invoke("init", 1); e != nil {
		t.Fatal(e)
	}
	if _, e := in.Invoke("sum", 1); e == nil || e.Error() != first {
		t.Fatalf("earlier trap changed: %v vs %s", e, first)
	}
}

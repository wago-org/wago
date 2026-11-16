//go:build amd64 && !tinygo

package wago

import (
	"bytes"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcLoopResolverModule(prefix []byte) []byte {
	return gcLoopResolverTypedModule(prefix, 0x7f, 11, 7, false)
}

func gcLoopResolverTypedModule(prefix []byte, storage, get byte, value int64, twice bool) []byte {
	result, constOp, add := wasm.I32, byte(0x41), byte(0x6a)
	resultByte := byte(0x7f)
	if storage == 0x7e {
		result, constOp, add, resultByte = wasm.I64, 0x42, 0x7c, 0x7e
	}
	// Parameters: count. Locals: array, index, total.
	body := []byte{3, 1, 0x63, 0, 1, 0x7f, 1, resultByte, 0x23, 0, 0x21, 1,
		0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40}
	body = append(body, prefix...)
	body = append(body, 0x20, 3, 0x20, 1, 0x20, 2, 0xfb, get, 0, add, 0x21, 3)
	if twice {
		body = append(body, 0x20, 3, 0x20, 1, 0x20, 2, 0x41, 1, 0x6a, 0xfb, get, 0, add, 0x21, 3)
	}
	body = append(body, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x0b, 0x20, 3, 0x0b)
	init := append([]byte{constOp}, wasmtest.SLEB64(value)...)
	init = append(init, 0x20, 0, 0xfb, 6, 0, 0x24, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, storage, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("sum", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), append(wasmtest.ULEB(uint32(len(body))), body...))),
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

func TestGCLoopResolverStorageAndSecondRead(t *testing.T) {
	for _, tc := range []struct {
		storage, get byte
		value        int64
		want         uint64
	}{
		{0x78, 12, -7, uint64(uint32(0xfffffff9))},
		{0x78, 13, -7, 249},
		{0x77, 12, -7, uint64(uint32(0xfffffff9))},
		{0x77, 13, -7, 65529},
		{0x7e, 11, 0x100000001, 0x100000001},
	} {
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLoopResolverTypedModule(nil, tc.storage, tc.get, tc.value, true))
		if e != nil {
			t.Fatal(e)
		}
		defer c.Close()
		in, e := Instantiate(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}})
		if e != nil {
			t.Fatal(e)
		}
		defer in.Close()
		for repeat := 0; repeat < 3; repeat++ {
			if _, e := in.Invoke("init", 33); e != nil {
				t.Fatal(e)
			}
			want := tc.want * 64
			if tc.storage != 0x7e {
				want = uint64(uint32(want))
			}
			if got, e := in.Invoke("sum", 32); e != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("storage%x get%d: %v %v want%d", tc.storage, tc.get, got, e, want)
			}
			if _, e := in.Invoke("sum", 33); e == nil {
				t.Fatal("second read bounds trap missing")
			}
		}
	}
}

func TestGCLoopResolverTrapPC(t *testing.T) {
	data := gcLoopResolverModule(nil)
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	in, e := Instantiate(c, InstantiateOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	_, e = in.Invoke("sum", 1)
	var trap *TrapError
	if !errors.As(e, &trap) || len(trap.Frames) != 1 {
		t.Fatalf("missing trap frame: %v", e)
	}
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	// Decoder stores the expression without local declarations; native source PC
	// uses the same function-relative instruction cursor.
	expected := uint32(bytes.Index(m.Code[1].BodyBytes, []byte{0xfb, 11, 0})) + m.Code[1].LocalDeclBytes
	if frame := trap.Frames[0]; !frame.HasProgramCounter || frame.ProgramCounter != expected {
		t.Fatalf("trap PC=%+v want%d", frame, expected)
	}
}

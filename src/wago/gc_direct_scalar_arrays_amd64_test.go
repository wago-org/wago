//go:build linux && amd64 && (wago_gc_direct_scalar_arrays || wago_gc_direct_trusted_arrays || wago_gc_direct_trusted_loop || wago_gc_direct_array_alloc || wago_gc_direct_trusted_refstruct || wago_gc_direct_trusted_alloc || (wago_gc_direct_fill || wago_gc_direct_fill_sse2) || wago_gc_direct_copy)

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func directScalarArrayModule(storage, valueType, get byte) []byte {
	init := []byte{0x41, 3, 0xfb, 7, 0, 0x24, 0, 0x0b}
	set := []byte{0x23, 0, 0x20, 0, 0x20, 1, 0xfb, 14, 0, 0x0b}
	read := []byte{0x23, 0, 0x20, 0, 0xfb, get, 0, 0x0b}
	churn := []byte{0xfb, 1, 1, 0x1a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, storage, 1}, []byte{0x5f, 0}, []byte{0x60, 0, 0}, []byte{0x60, 2, 0x7f, valueType, 0}, []byte{0x60, 1, 0x7f, 1, valueType})),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3}, []byte{4}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("set", 0, 1), wasmtest.ExportEntry("get", 0, 2), wasmtest.ExportEntry("churn", 0, 3))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), wasmtest.Code(set), wasmtest.Code(read), wasmtest.Code(churn))),
	)
}

func newDirectScalarArrayInstance(t *testing.T, data []byte) *Instance {
	t.Helper()
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	rt := NewRuntime()
	t.Cleanup(func() { rt.Close() })
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { in.Close() })
	return in
}

func TestDirectGCScalarArrayOperations(t *testing.T) {
	for _, tc := range []struct {
		name                string
		storage, param, get byte
		arg, want           uint64
	}{
		{"i8s", 0x78, 0x7f, 12, 255, 0xffffffff}, {"i8u", 0x78, 0x7f, 13, 255, 255},
		{"i16s", 0x77, 0x7f, 12, 65535, 0xffffffff}, {"i16u", 0x77, 0x7f, 13, 65535, 65535},
		{"i32", 0x7f, 0x7f, 11, 0xdeadbeef, 0xdeadbeef}, {"i64", 0x7e, 0x7e, 11, 0x123456789abcdef, 0x123456789abcdef},
		{"f32", 0x7d, 0x7d, 11, uint64(math.Float32bits(-1.25)), uint64(math.Float32bits(-1.25))},
		{"f64", 0x7c, 0x7c, 11, math.Float64bits(-1.25), math.Float64bits(-1.25)},
		{"f32-nan", 0x7d, 0x7d, 11, 0x7fc12345, 0x7fc12345},
		{"f64-nan", 0x7c, 0x7c, 11, 0x7ff8123456789abc, 0x7ff8123456789abc},
		{"f64-negative-zero", 0x7c, 0x7c, 11, 0x8000000000000000, 0x8000000000000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := newDirectScalarArrayInstance(t, directScalarArrayModule(tc.storage, tc.param, tc.get))
			if _, e := in.Invoke("init"); e != nil {
				t.Fatal(e)
			}
			const sentinel = 0x81234567
			slot := in.ctrl[runtime.HostCtrlImportIndexOffset:]
			for n := 0; n < 5; n++ {
				index := uint64(n%3) | (uint64(1) << 32)
				binary.LittleEndian.PutUint32(slot, sentinel)
				if _, e := in.Invoke("set", index, tc.arg); e != nil {
					t.Fatal(e)
				}
				if binary.LittleEndian.Uint32(slot) != sentinel {
					t.Fatal("store used helper")
				}
				if _, e := in.Invoke("churn"); e != nil {
					t.Fatal(e)
				}
				binary.LittleEndian.PutUint32(slot, sentinel)
				got, e := in.Invoke("get", index)
				if e != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatal(got, e, tc.want)
				}
				if binary.LittleEndian.Uint32(slot) != sentinel {
					t.Fatal("load used helper")
				}
			}
			if in.gc.Stats().FullCollections == 0 {
				t.Fatal("no relocation")
			}
		})
	}
}

func TestDirectGCScalarArrayBoundsAndNull(t *testing.T) {
	data := directScalarArrayModule(0x7e, 0x7e, 11)
	in := newDirectScalarArrayInstance(t, data)
	// Null traps precede even an invalid unsigned index, at the array opcode.
	m, e := wasm.DecodeModule(data)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"get", "set"} {
		args := []uint64{0xffffffff}
		idx := 2
		op := byte(11)
		if name == "set" {
			args = append(args, 99)
			idx = 1
			op = 14
		}
		_, e := in.Invoke(name, args...)
		var trap *TrapError
		if !errors.As(e, &trap) {
			t.Fatal("missing null trap", e)
		}
		want := uint32(bytes.Index(m.Code[idx].BodyBytes, []byte{0xfb, op, 0})) + m.Code[idx].LocalDeclBytes
		if len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != want {
			t.Fatalf("null PC %+v want %d", trap.Frames, want)
		}
	}
	if _, e := in.Invoke("init"); e != nil {
		t.Fatal(e)
	}
	if _, e := in.Invoke("set", 1, 77); e != nil {
		t.Fatal(e)
	}
	checkUnchanged := func() {
		got, e := in.Invoke("get", 1)
		if e != nil || len(got) != 1 || got[0] != 77 {
			t.Fatal("failed store mutated array", got, e)
		}
	}
	for _, idx := range []uint64{3, 0xffffffff} {
		if _, e := in.Invoke("set", idx, 99); e == nil {
			t.Fatal("accepted bad store index")
		}
		if _, e := in.Invoke("get", idx); e == nil {
			t.Fatal("accepted bad read index")
		}
		checkUnchanged()
	}
}

func TestDirectGCScalarArrayZeroLength(t *testing.T) {
	data := directScalarArrayModule(0x7f, 0x7f, 11)
	data = bytes.Replace(data, []byte{0x41, 3, 0xfb, 7, 0}, []byte{0x41, 0, 0xfb, 7, 0}, 1)
	in := newDirectScalarArrayInstance(t, data)
	if _, e := in.Invoke("init"); e != nil {
		t.Fatal(e)
	}
	for _, index := range []uint64{0, 0xffffffff} {
		if _, e := in.Invoke("get", index); e == nil {
			t.Fatal("zero-length read accepted")
		}
		if _, e := in.Invoke("set", index, 1); e == nil {
			t.Fatal("zero-length write accepted")
		}
	}
}

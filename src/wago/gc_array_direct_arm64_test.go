//go:build (linux || darwin) && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"strings"
	"testing"
)

func TestGCNativeScalarArrayFillBounds(t *testing.T) {
	// (start, count, index, value) -> array.fill then array.get. A fresh array
	// makes successful nursery access repeatable; tiny/stress exercise fallback.
	body := []byte{1, 1, 0x63, 0, 0x41, 8, 0xfb, 7, 0, 0x21, 4,
		0x20, 4, 0x20, 0, 0x20, 3, 0x20, 1, 0xfb, 16, 0,
		0x20, 4, 0x20, 2, 0xfb, 11, 0, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cfg := range []GCConfig{{}, {CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true}} {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			start, count, index, value, want uint64
			trap                             bool
		}{
			{0, 8, 7, 0xffffffff, 0xffffffff, false}, {2, 3, 2, 37, 37, false}, {2, 3, 1, 37, 0, false}, {8, 0, 0, 7, 0, false},
			{9, 0, 0, 7, 0, true}, {7, 2, 0, 7, 0, true}, {0, 0xffffffff, 0, 7, 0, true}, {0, 8, 8, 7, 0, true},
		} {
			got, err := in.Invoke("run", tc.start, tc.count, tc.index, tc.value)
			if tc.trap {
				var trap *TrapError
				if !errors.As(err, &trap) {
					t.Fatalf("%+v: expected trap, got %v %v", tc, got, err)
				}
			} else if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("%+v: got %v %v", tc, got, err)
			}
		}
		in.Close()
	}
}

// These compact Wasm fixtures exercise allocation and fill in guest loops,
// allowing reservations to be consumed between public invocation boundaries.
func TestGCNativeScalarArrayAllocationLoop(t *testing.T) {
	for _, encoded := range []string{
		"0061736d010000000109025e7f0160017f017f03020101070d010962656e63686d61726b00000a2d012b01027f0340200220014110fb06002001410f71fb0b006a2102200141016a210120012000490d000b20020b0024046e616d65020c01000300016e010169020161030901000100046c6f6f70040401000161",
		"0061736d010000000109025e7f0160017f017f03020101070d010962656e63686d61726b00000a42014002027f01630003402002027f41004110fb060021032003410020014110fb100020032001410f71fb0b000b6a2102200141016a210120012000490d000b20020b0027046e616d65020f01000400016e010169020161030172030901000100046c6f6f70040401000161",
	} {
		data, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {ThroughputHeapBytes: 64 << 10, VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true}} {
			in, err := Instantiate(c, InstantiateOptions{GC: cfg})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 5; i++ {
				got, err := in.Invoke("benchmark", 1000)
				if err != nil || len(got) != 1 || got[0] != 499500 {
					t.Fatalf("loop %d: %v %v", i, got, err)
				}
				if n := in.gc.Stats().Allocations; n != uint64((i+1)*1000) {
					t.Fatalf("allocations=%d", n)
				}
			}
			in.Close()
		}
		c.Close()
	}
}

func TestGCNativeArrayAllocationLengthsAndWideBits(t *testing.T) {
	body := []byte{2, 1, 0x7f, 1, 0x7e,
		0x03, 0x40,
		0x20, 1, 0x20, 0, 0xfb, 6, 0,
		0x20, 0, 0x41, 1, 0x6b, 0xfb, 11, 0, 0x21, 3,
		0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x41, 0xc8, 1, 0x49, 0x0d, 0,
		0x0b, 0x20, 3, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7e, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	const want = uint64(0xfedcba9876543210)
	for _, length := range []uint64{1, 2, 3, 4, 5, 7, 15, 16, 17, 63, 64, 65, 127} {
		got, err := in.Invoke("run", length, want)
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("length %d: %x %v", length, got, err)
		}
	}
	if got := in.gc.Stats().Allocations; got != 13*200 {
		t.Fatalf("allocations=%d", got)
	}
}

func TestGCArrayConstructorForwardBoundsAndLocalMutation(t *testing.T) {
	for _, tc := range []struct {
		length, mask, index int32
		overwrite           bool
		trap                bool
	}{
		{16, 15, 123, false, false}, {3, 1, 3, false, false}, {3, 3, 3, false, true},
		{0, 0, 0, false, true}, {16, -1, 16, false, true}, {16, 15, 9, true, false},
	} {
		body := []byte{0, 0x20, 0}
		if tc.overwrite {
			body = append(body, 0x41, 7, 0x21, 0)
		}
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(tc.length)...)
		body = append(body, 0xfb, 6, 0, 0x20, 1, 0x41)
		body = append(body, wasmtest.SLEB32(tc.mask)...)
		body = append(body, 0x71, 0xfb, 11, 0, 0x0b)
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("run", 0x87654321, uint64(uint32(tc.index)))
		if tc.trap {
			var trap *TrapError
			if !errors.As(err, &trap) {
				t.Fatalf("%+v: got %v %v, want bounds trap", tc, got, err)
			}
		} else if err != nil || len(got) != 1 || got[0] != 0x87654321 {
			t.Fatalf("%+v: got %v %v", tc, got, err)
		}
		if n := in.gc.Stats().Allocations; n != 1 {
			t.Fatalf("%+v: allocations=%d", tc, n)
		}
		in.Close()
		if tc.length > 0 {
			tiny, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = tiny.Invoke("run", 0x87654321, uint64(uint32(tc.index)))
			if err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
				t.Fatalf("forwarded allocation exhaustion: %v", err)
			}
			tiny.Close()
		}
		c.Close()
	}
}

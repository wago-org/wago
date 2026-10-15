//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcConstructorForwardBytes() []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00},                   // open base: (struct (field i32))
		{0x50, 0x01, 0x00, 0x5f, 0x02, 0x7f, 0x00, 0x7f, 0x00}, // open child <: base
	}
	body := []byte{
		0x00,       // no locals
		0x20, 0x00, // local.get 0
		0x41, 0x07, // i32.const 7
		0xfb, 0x00, 0x01, // struct.new child
		0xfb, 0x16, 0x00, // ref.cast base
		0xfb, 0x02, 0x00, 0x00, // struct.get base field 0
		0x0b,
	}
	return gcInstructionBenchmarkModuleWithTypes(types, body)
}

func TestGCConstructorGetForwardExecution(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcConstructorForwardBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	profiles := []struct {
		name string
		gc   GCConfig
	}{
		{name: "throughput", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			instance, err := Instantiate(compiled, InstantiateOptions{GC: tc.gc})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			for i := 0; i < 1000; i++ {
				want := uint64(uint32(i*17 - 33))
				got, err := instance.Invoke("run", want)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("run iteration %d = %v, %v; want [%d]", i, got, err, want)
				}
			}
		})
	}
}

func gcFreshStructLocalForwardBytes(overwriteSource, mutate bool, rootLength byte) []byte {
	fieldMutability := byte(0x00)
	if mutate {
		fieldMutability = 0x01
	}
	types := [][]byte{
		{0x5f, 0x01, 0x7f, fieldMutability}, // (struct (field i32))
		{0x5e, 0x63, 0x00, 0x01},            // (array (mut (ref null 0)))
	}
	body := []byte{
		0x02, 0x01, 0x63, 0x01, 0x01, 0x63, 0x00, // locals: roots, item
		0x41, rootLength, 0xfb, 0x07, 0x01, 0x21, 0x01, // roots = array.new_default 1 (rootLength)
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x02, // item = struct.new 0 (param)
	}
	if overwriteSource {
		body = append(body, 0x41, 0x07, 0x21, 0x00) // direct source-local overwrite
	}
	body = append(body,
		0x20, 0x01, 0x41, 0x00, 0x20, 0x02, 0xfb, 0x0e, 0x01, // roots[0] = item
	)
	if mutate {
		body = append(body, 0x20, 0x02, 0x41, 0x07, 0xfb, 0x05, 0x00, 0x00) // item.field 0 = 7
	}
	body = append(body,
		0x20, 0x02, 0xfb, 0x02, 0x00, 0x00, // item.field 0
		0x0b,
	)
	return gcInstructionBenchmarkModuleWithTypes(types, body)
}

func TestGCFreshStructLocalGetForwardExecution(t *testing.T) {
	for _, tc := range []struct {
		name            string
		overwriteSource bool
		mutate          bool
		want            func(int) uint64
	}{
		{name: "forward", want: func(i int) uint64 { return uint64(uint32(i*17 - 33)) }},
		{name: "source overwritten", overwriteSource: true, want: func(i int) uint64 { return uint64(uint32(i*17 - 33)) }},
		{name: "field mutated", mutate: true, want: func(int) uint64 { return 7 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshStructLocalForwardBytes(tc.overwriteSource, tc.mutate, 1))
			if err != nil {
				t.Fatal(err)
			}
			for _, gc := range []GCConfig{
				{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
				{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
			} {
				instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 100; i++ {
					input := uint64(uint32(i*17 - 33))
					want := tc.want(i)
					got, err := instance.Invoke("run", input)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("iteration %d = %v, %v; want [%d]", i, got, err, want)
					}
				}
				if err := instance.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGCFreshStructRootStoreTrap(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshStructLocalForwardBytes(false, false, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run", 1); err == nil {
		t.Fatalf("run = %v, want out-of-bounds trap", got)
	}
}

func gcFreshArrayLocalForwardBytes(rootLength, length, mask byte, overwriteSource, mutate, verifyPayload bool) []byte {
	types := [][]byte{
		{0x5e, 0x7f, 0x01},       // (array (mut i32))
		{0x5e, 0x63, 0x00, 0x01}, // (array (mut (ref null 0)))
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
	}
	body := []byte{
		0x02, 0x01, 0x63, 0x01, 0x01, 0x63, 0x00, // locals: roots, item
		0x41, rootLength, 0xfb, 0x07, 0x01, 0x21, 0x02,
		0x20, 0x00, 0x41, length, 0xfb, 0x06, 0x00, 0x21, 0x03,
	}
	if overwriteSource {
		body = append(body, 0x41, 0x07, 0x21, 0x00) // direct source-local overwrite
	}
	body = append(body, 0x20, 0x02, 0x41, 0x00, 0x20, 0x03, 0xfb, 0x0e, 0x01)
	if mutate {
		body = append(body, 0x20, 0x03, 0x41, 0x00, 0x41, 0x07, 0xfb, 0x0e, 0x00)
	}
	body = append(body, 0x20, 0x03, 0x20, 0x01, 0x41, mask, 0x71, 0xfb, 0x0b, 0x00, 0x0b)
	if verifyPayload {
		body = body[:len(body)-1]
		body = append(body, 0x20, 0x03, 0x20, 0x01, 0x41, mask, 0x71, 0xfb, 0x0b, 0x00, 0x6a, 0x0b)
	}
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(body)),
	)
}

func TestGCFreshArrayLocalGetForwardExecution(t *testing.T) {
	for _, tc := range []struct {
		name            string
		overwriteSource bool
		mutate          bool
	}{
		{name: "forward"},
		{name: "source overwritten", overwriteSource: true},
		{name: "array mutated", mutate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshArrayLocalForwardBytes(1, 16, 15, tc.overwriteSource, tc.mutate, true))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			for _, gc := range []GCConfig{
				{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
				{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
			} {
				instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
				if err != nil {
					t.Fatal(err)
				}
				for index := uint64(0); index < 16; index++ {
					value := uint64(uint32(0x91230000 + index))
					want := uint64(uint32(value * 2))
					if tc.mutate && index == 0 {
						want = 14
					}
					got, err := instance.Invoke("run", value, index)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("profile %d index %d = %v, %v; want [%#x]", gc.Profile, index, got, err, want)
					}
				}
				if got := instance.gc.Stats().Allocations; got != 32 {
					t.Fatalf("profile %d allocations = %d, want 32", gc.Profile, got)
				}
				if err := instance.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestGCFreshArrayUnsafeMaskTraps(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshArrayLocalForwardBytes(1, 16, 31, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run", 9, 16)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
		t.Fatalf("run = %v, %v; want bounds trap", got, err)
	}
}

func TestGCFreshArrayRootStoreTrap(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFreshArrayLocalForwardBytes(0, 16, 15, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run", 9, 0)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
		t.Fatalf("run = %v, %v; want root-store bounds trap", got, err)
	}
}

//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func repeatedCollectorCastBytes(kind string, casts [][]byte) []byte {
	object := []byte{0x5f, 1, 0x7f, 0}
	constructor := []byte{0x41, 7, 0xfb, 0, 0}
	access := []byte{0xfb, 2, 0, 0}
	if kind != "struct" {
		object = []byte{0x5e, 0x7f, 1}
		constructor = []byte{0x41, 7, 0x41, 3, 0xfb, 6, 0}
		access = []byte{0xfb, 15}
		if kind == "array-get" {
			access = []byte{0x41, 0, 0xfb, 11, 0}
		}
	}
	body := []byte{0x20, 0}
	for _, cast := range casts {
		body = append(body, cast...)
	}
	body = append(body, access...)
	body = append(body, 0x0b)
	callRead := func(prefix []byte) []byte {
		return wasmtest.Code(append(append([]byte(nil), prefix...), 0x10, 0, 0x0b))
	}
	roots := append(append([]byte(nil), constructor...), 0x10, 6, 0x1a, 0x10, 0, 0x0b)
	bodies := [][]byte{
		wasmtest.Code(body),
		callRead(constructor),
		callRead([]byte{0xd0, 0x6e}),
		callRead([]byte{0x41, 3, 0xfb, 0x1c}),
		callRead([]byte{0xfb, 1, 1}),
		wasmtest.Code(roots),
		// This object escapes through a global, so collection cannot be removed
		// as a dead allocation while the caller's first object is still live.
		wasmtest.Code([]byte{0xfb, 1, 1, 0x24, 0, 0x23, 0, 0x0b}),
	}
	funcs := [][]byte{{2}, {3}, {3}, {3}, {3}, {3}, {4}}
	exports := [][]byte{}
	for i, name := range []string{"run", "null", "i31", "wrong", "roots"} {
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i+1)))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(object, []byte{0x5f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.AnyRef, true, []byte{0xd0, 0x6e, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCRepeatedCastRuntime(t *testing.T) {
	for _, kind := range []string{"struct", "array-get", "array-len"} {
		for _, nullable := range []bool{false, true} {
			for _, copies := range []int{2, 4} {
				t.Run(fmt.Sprintf("%s/nullable=%t/casts=%d", kind, nullable, copies), func(t *testing.T) {
					sub := byte(22)
					if nullable {
						sub = 23
					}
					casts := make([][]byte, copies)
					for i := range casts {
						casts[i] = []byte{0xfb, sub, 0}
					}
					compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), repeatedCollectorCastBytes(kind, casts))
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					if admission := compiled.GCNativeRootAdmission(); !admission.Required || !admission.Exact {
						t.Fatalf("missing exact roots: %+v", admission)
					}
					configs := []GCConfig{
						{CollectEveryAlloc: true, ForceMajorEveryMinor: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
						{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
					}
					for _, config := range configs {
						in, err := Instantiate(compiled, InstantiateOptions{GC: config})
						if err != nil {
							t.Fatal(err)
						}
						func() {
							defer in.Close()
							want := uint64(7)
							if kind == "array-len" {
								want = 3
							}
							for range 3 {
								if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != want {
									t.Fatalf("run = %v, %v; want [%d]", got, err, want)
								}
								before := in.gc.Stats().FullCollections
								if got, err := in.Invoke("roots"); err != nil || len(got) != 1 || got[0] != want {
									t.Fatalf("roots = %v, %v; want [%d]", got, err, want)
								}
								if config.CollectEveryAlloc && in.gc.Stats().FullCollections-before < 2 {
									t.Fatal("root check did not collect while the first object was live")
								}
								if err := in.CollectGC(); err != nil {
									t.Fatal(err)
								}
							}
							for _, name := range []string{"null", "i31", "wrong"} {
								_, err := in.Invoke(name)
								var trap *TrapError
								want := TrapCastFailure
								if name == "null" && nullable {
									want = TrapNullReference
								}
								if !errors.As(err, &trap) || trap.Code != want {
									t.Fatalf("%s trap = %v; want %v", name, err, want)
								}
							}
						}()
					}
				})
			}
		}
	}
}

func TestGCRepeatedCastKeepsNullabilityChange(t *testing.T) {
	data := repeatedCollectorCastBytes("struct", [][]byte{{0xfb, 23, 0}, {0xfb, 22, 0}})
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_, err = in.Invoke("null")
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
		t.Fatalf("nullable then non-null cast = %v; want cast failure", err)
	}
}

func TestGCRepeatedCastKeepsExactnessChange(t *testing.T) {
	base := []byte{0x50, 0, 0x5f, 1, 0x7f, 1}
	child := []byte{0x4f, 1, 0, 0x5f, 1, 0x7f, 1}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(base, child, wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0xfb, 22, 0x62, 0, 0xfb, 2, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 1, 0x10, 0, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_, err = in.Invoke("run")
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
		t.Fatalf("ordinary then exact base cast accepted a proper subtype: %v", err)
	}
}

func TestGCRepeatedCastKeepsTargetChange(t *testing.T) {
	base := []byte{0x50, 0, 0x5f, 1, 0x7f, 1}
	child := []byte{0x4f, 1, 0, 0x5f, 2, 0x7f, 1, 0x7f, 1}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(base, child, wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0xfb, 22, 1, 0x1a, 0x41, 1, 0x0b}),
			wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x10, 0, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_, err = in.Invoke("run")
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
		t.Fatalf("base then child cast accepted a base object: %v", err)
	}
}

func TestGCRepeatedCastReferenceReadRoot(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 1, 0x6e, 1}, []byte{0x5f, 1, 0x7f, 0}, []byte{0x5f, 0},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{3}, []byte{4})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.AnyRef, true, []byte{0xd0, 0x6e, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 1, 0xfb, 0, 0,
				0xfb, 22, 0, 0xfb, 22, 0, 0xfb, 2, 0, 0,
				0x10, 1, 0x1a,
				0xfb, 22, 1, 0xfb, 22, 1, 0xfb, 2, 1, 0, 0x0b}),
			wasmtest.Code([]byte{0xfb, 1, 2, 0x24, 0, 0x23, 0, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if admission := compiled.GCNativeRootAdmission(); !admission.Exact || admission.Callsites == 0 {
		t.Fatalf("missing source call roots: %+v", admission)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, ForceMajorEveryMinor: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	before := in.gc.Stats().FullCollections
	if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 7 {
		t.Fatalf("retained field reference = %v, %v; want [7]", got, err)
	}
	if in.gc.Stats().FullCollections-before < 3 {
		t.Fatal("field reference was not tested across the later allocation's collection")
	}
}

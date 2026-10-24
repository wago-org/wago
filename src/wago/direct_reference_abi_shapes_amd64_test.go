//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type directReferenceABIShape struct {
	name     string
	params   []wasm.ValType
	results  []wasm.ValType
	register bool
}

func directReferenceABIShapes() []directReferenceABIShape {
	ref := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	return []directReferenceABIShape{
		{"seven-gp", []wasm.ValType{ref, ref, ref, ref, ref, ref, ref}, []wasm.ValType{wasm.I32}, true},
		{"eight-gp-wrapper", []wasm.ValType{ref, ref, ref, ref, ref, ref, ref, ref}, []wasm.ValType{wasm.I32}, false},
		{"mixed", []wasm.ValType{ref, wasm.F32, wasm.F64, wasm.I32, ref}, []wasm.ValType{wasm.I32}, true},
		{"reference-result", []wasm.ValType{ref, ref, ref, ref, ref, ref, ref}, []wasm.ValType{ref}, true},
		{"pair-wrapper", []wasm.ValType{ref, ref}, []wasm.ValType{ref, ref}, false},
		{"vector-wrapper", []wasm.ValType{ref, wasm.V128}, []wasm.ValType{wasm.I32}, false},
		{"void", []wasm.ValType{ref}, nil, true},
	}
}

func directReferenceABIShapeModule(shape directReferenceABIShape) ([]byte, uint64) {
	target := []byte{0xfb, 1, 0, 0x1a} // keep all used params live across collection
	caller := []byte{1, 1, 0x63, 0}    // one reference local for the pair result
	want := uint64(0)
	for i, typ := range shape.params {
		n := i + 1
		switch {
		case typ.Kind() == wasm.ValRef:
			caller = append(caller, 0x41, byte(n), 0xfb, 0, 0)
		case wasm.EqualValType(typ, wasm.F32):
			caller = append(caller, 0x43)
			caller = binary.LittleEndian.AppendUint32(caller, math.Float32bits(float32(n)))
		case wasm.EqualValType(typ, wasm.F64):
			caller = append(caller, 0x44)
			caller = binary.LittleEndian.AppendUint64(caller, math.Float64bits(float64(n)))
		case wasm.EqualValType(typ, wasm.V128):
			caller = append(caller, 0xfd, 0x0c)
			caller = binary.LittleEndian.AppendUint64(caller, uint64(n))
			caller = binary.LittleEndian.AppendUint64(caller, 0)
		default:
			caller = append(caller, 0x41, byte(n))
		}
		// Observe every parameter after collection, with distinct position weights.
		target = append(target, 0x20, byte(i))
		switch {
		case typ.Kind() == wasm.ValRef:
			target = append(target, 0xfb, 2, 0, 0)
		case wasm.EqualValType(typ, wasm.F32):
			target = append(target, 0xa8)
		case wasm.EqualValType(typ, wasm.F64):
			target = append(target, 0xaa)
		case wasm.EqualValType(typ, wasm.V128):
			target = append(target, 0xfd, 0x1b, 0)
		}
		target = append(target, 0x41, byte(n), 0x6c)
		if i > 0 {
			target = append(target, 0x6a)
		}
		want += uint64(n * n)
	}
	caller = append(caller, 0x10, 0)
	switch {
	case len(shape.results) == 0:
		target = append(target, 0x1a)
		caller = append(caller, 0x41, 42)
		want = 42
	case shape.results[0].Kind() == wasm.ValRef:
		// Check parameter transfer before producing the requested reference shape.
		target = append(target, 0x41)
		target = append(target, wasmtest.SLEB32(int32(want))...)
		target = append(target, 0x47, 0x04, 0x40, 0x00, 0x0b)
		if len(shape.results) == 2 {
			target = append(target, 0x20, 0)
			caller = append(caller, 0x21, 0, 0xfb, 2, 0, 0, 0x20, 0, 0xfb, 2, 0, 0, 0x6a)
			want = uint64(1 + len(shape.params))
		} else {
			caller = append(caller, 0xfb, 2, 0, 0)
			want = uint64(len(shape.params))
		}
		target = append(target, 0x20, byte(len(shape.params)-1))
	}
	target = append(target, 0x0b)
	caller = append(caller, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, directReferenceABIFuncType(shape.params, shape.results), directReferenceABIFuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), append(wasmtest.ULEB(uint32(len(caller))), caller...))),
	), want
}

func TestDirectReferenceABIShapes(t *testing.T) {
	for _, shape := range directReferenceABIShapes() {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled%t", shape.name, enabled), func(t *testing.T) {
				data, want := directReferenceABIShapeModule(shape)
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("dead-gc-new", false).WithOptimization("direct-reference-abi", enabled), data)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				loaded := publicArtifactRoundTrip(t, c)
				defer loaded.Close()
				for _, candidate := range []*Compiled{c, loaded} {
					if status := candidate.GCNativeRootAdmission(); !status.Required || !status.Exact {
						t.Fatalf("exact roots lost: %+v", status)
					}
					for _, profile := range directReferenceABIGCProfiles() {
						in, err := Instantiate(candidate, InstantiateOptions{GC: profile.gc})
						if err != nil {
							t.Fatal(err)
						}
						got, err := in.Invoke("run")
						in.Close()
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("%s run=%v,%v, want [%d]", profile.name, got, err, want)
						}
					}
				}
			})
		}
	}
}

func TestDirectReferenceABINullAndI31(t *testing.T) {
	for _, tc := range []struct {
		name        string
		typ         wasm.ValType
		value, read []byte
		want        uint64
	}{
		{"null", wasm.AnyRef, []byte{0xd0, 0x6e}, []byte{0xd1}, 1},
		{"i31", wasm.I31Ref, []byte{0x41, 42, 0xfb, 0x1c}, []byte{0xfb, 0x1d}, 42},
	} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled%t", tc.name, enabled), func(t *testing.T) {
				caller := append(append([]byte(nil), tc.value...), 0x10, 0)
				caller = append(caller, 0xfb, 1, 0, 0x1a)
				caller = append(caller, tc.read...)
				caller = append(caller, 0x0b)
				data := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, directReferenceABIFuncType([]wasm.ValType{tc.typ}, []wasm.ValType{tc.typ}), directReferenceABIFuncType(nil, []wasm.ValType{wasm.I32}))),
					wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0x0b}), wasmtest.Code(caller))),
				)
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("dead-gc-new", false).WithOptimization("direct-reference-abi", enabled), data)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				loaded := publicArtifactRoundTrip(t, c)
				defer loaded.Close()
				for _, candidate := range []*Compiled{c, loaded} {
					for _, profile := range directReferenceABIGCProfiles() {
						in, err := Instantiate(candidate, InstantiateOptions{GC: profile.gc})
						if err != nil {
							t.Fatal(err)
						}
						got, err := in.Invoke("run")
						in.Close()
						if err != nil || len(got) != 1 || got[0] != tc.want {
							t.Fatalf("%s run=%v,%v, want [%d]", profile.name, got, err, tc.want)
						}
					}
				}
			})
		}
	}
}

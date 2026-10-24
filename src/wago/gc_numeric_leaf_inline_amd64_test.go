//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcNumericLeafTrapModule() []byte {
	globals := make([][]byte, 8)
	for i := range globals {
		globals[i] = wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b})
	}
	bodies := [][]byte{
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6d, 0x0b}), // i32.div_s
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x7f, 0x0b}), // i64.div_s
	}
	for leaf := byte(0); leaf < 2; leaf++ {
		body := []byte{0x03, 0x40}
		for g := byte(0); g < 8; g++ {
			body = append(body, 0x23, g, 0x42, g+1, 0x7c, 0x24, g)
		}
		body = append(body, 0x0b, 0x20, 0, 0x20, 1, 0x10, leaf, 0x0b)
		bodies = append(bodies, wasmtest.Code(body))
	}
	// A GC allocation requires the module's exact native root plan.
	bodies = append(bodies, wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1},
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{1}, []byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("i32", 0, 2), wasmtest.ExportEntry("i64", 0, 3))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCNumericLeafInlineTrapGlobals(t *testing.T) {
	for _, inline := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", inline), gcNumericLeafTrapModule())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if admission := c.GCNativeRootAdmission(); !admission.Exact || !admission.Required {
			t.Fatalf("trap fixture must use exact roots: %+v", admission)
		}
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for ai, artifact := range []*Compiled{c, loaded} {
			for _, tc := range []struct {
				export string
				x, y   uint64
				trap   TrapCode
			}{
				{"i32", 42, 0, TrapDivZero}, {"i32", 1 << 31, 0xffffffff, TrapDivOverflow},
				{"i64", 42, 0, TrapDivZero}, {"i64", 1 << 63, ^uint64(0), TrapDivOverflow},
				{"i32", 42, 2, TrapNone}, {"i64", 42, 2, TrapNone},
			} {
				t.Run(fmt.Sprintf("inline-%t/artifact-%d/%s/%d", inline, ai, tc.export, tc.trap), func(t *testing.T) {
					in, err := Instantiate(artifact)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					got, err := in.Invoke(tc.export, tc.x, tc.y)
					if tc.trap == TrapNone {
						if err != nil || len(got) != 1 || got[0] != 21 {
							t.Fatalf("divide = %v, %v; want [21]", got, err)
						}
					} else {
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != tc.trap {
							t.Fatalf("divide trap = %v; want %v", err, tc.trap)
						}
					}
					for g := 0; g < 8; g++ {
						if got := readGlobalObject(in.globalCells[g], in.c.Globals[g].Type); got != uint64(g+1) {
							t.Errorf("global %d = %d; want %d", g, got, g+1)
						}
					}
				})
			}
		}
	}
}

func gcNumericLeafInlineModule() []byte {
	// add, wide (with a declared local), allocate, roots, loop.
	add := wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6a, 0x0b})
	wideBody := []byte{1, 1, 0x7e, 0x20, 0, 0x42, 3, 0x7c, 0x21, 1, 0x20, 1, 0x0b}
	wide := append(wasmtest.ULEB(uint32(len(wideBody))), wideBody...)
	allocate := wasmtest.Code([]byte{0x41, 0, 0xfb, 0, 0, 0x1a, 0x0b})
	rootsBody := []byte{2, 2, 0x63, 0, 1, 0x7f, // two nullable struct locals and one i32
		0x20, 0, 0xfb, 0, 0, 0x21, 1,
		0x10, 2, // first allocating call: only local 1 is live
		0x20, 0, 0x41, 7, 0x10, 0, 0x21, 3,
		0x41, 0xe8, 7, 0xfb, 0, 0, 0x21, 2,
		0x20, 0, 0xad, 0x10, 1, 0xa7, 0x20, 3, 0x6a, 0x21, 3,
		0x10, 2, // second allocating call: locals 1 and 2 are both live
		0x20, 1, 0xfb, 2, 0, 0, 0x20, 2, 0xfb, 2, 0, 0, 0x6a, 0x20, 3, 0x6a, 0x0b}
	roots := append(wasmtest.ULEB(uint32(len(rootsBody))), rootsBody...)
	loopBody := []byte{1, 2, 0x7f, 0x02, 0x40, 0x03, 0x40,
		0x20, 2, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 1, 0x41, 1, 0x10, 0, 0x21, 1,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b}
	loop := append(wasmtest.ULEB(uint32(len(loopBody))), loopBody...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1},
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3}, []byte{4}, []byte{4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roots", 0, 3), wasmtest.ExportEntry("loop", 0, 4))),
		wasmtest.Section(10, wasmtest.Vec(add, wide, allocate, roots, loop)),
	)
}

func TestGCNumericLeafInlineRoots(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(fmt.Sprintf("inline-%t", inline), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", inline), gcNumericLeafInlineModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if admission := c.GCNativeRootAdmission(); !admission.Exact || c.genericGCFrameRoots() == nil {
				t.Fatalf("numeric inline module must retain exact roots: %+v", admission)
			}
			loaded := publicArtifactRoundTrip(t, c)
			defer loaded.Close()
			for _, artifact := range []*Compiled{c, loaded} {
				if admission := artifact.GCNativeRootAdmission(); !admission.Exact {
					t.Fatalf("artifact lost exact roots: %+v", admission)
				}
				for _, cfg := range []GCConfig{
					{CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressNurseryBytes: 128},
					{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
				} {
					in, err := Instantiate(artifact, InstantiateOptions{GC: cfg})
					if err != nil {
						t.Fatal(err)
					}
					func() {
						defer in.Close()
						for _, v := range []uint64{0, 17, 99} {
							got, err := in.Invoke("roots", v)
							if err != nil || len(got) != 1 || got[0] != 1010+3*v {
								t.Fatalf("roots(%d) = %v, %v; want %d", v, got, err, 1010+3*v)
							}
						}
						if got, err := in.Invoke("loop", 8192); err != nil || len(got) != 1 || got[0] != 8192 {
							t.Fatalf("loop = %v, %v; want [8192]", got, err)
						}
					}()
				}
			}
		})
	}
}

func BenchmarkGCNumericLeafInline(b *testing.B) {
	for _, inline := range []bool{false, true} {
		b.Run(fmt.Sprintf("inline-%t", inline), func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", inline), gcNumericLeafInlineModule())
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			if admission := c.GCNativeRootAdmission(); !admission.Exact {
				b.Fatalf("benchmark requires exact roots: %+v", admission)
			}
			in, err := Instantiate(c)
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			invoke := func() {
				got, err := in.Invoke("loop", 8192)
				if err != nil || len(got) != 1 || got[0] != 8192 {
					b.Fatalf("loop = %v, %v; want [8192]", got, err)
				}
			}
			invoke()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				invoke()
			}
		})
	}
}

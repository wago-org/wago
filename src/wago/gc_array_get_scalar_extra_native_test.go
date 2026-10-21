//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCGeneralScalarArrayGetVectorCodecJoins(t *testing.T) {
	body := []byte{1, 1, 0x7e, 0x42, 55, 0x21, 2, 0xfd, 12, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 0x42, 42, 0x20, 1, 0x04, 0x7e,
		0x23, 0, 0x20, 0, 0xfb, 11, 0, 0x05, 0x23, 0, 0x20, 0, 0xfb, 11, 0, 0x0b, 0x20, 2, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x50, 0, 0x5e, 0x7e, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.V128, wasm.I64, wasm.I64, wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0xfb, 6, 0, 0x24, 0, 0x0b}), append(wasmtest.ULEB(uint32(len(body))), body...))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for _, compiled := range []*Compiled{c, loaded} {
		in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 256, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		const bits = uint64(0x7ff8123456789abc)
		if _, err := in.Invoke("init", 17, bits); err != nil {
			t.Fatal(err)
		}
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
		view := in.gc.NativeView()
		count := view.HandleCount
		for _, fallback := range []bool{false, true} {
			for _, branch := range []uint64{0, 1} {
				if fallback {
					view.HandleCount = 0
				}
				got, err := in.Invoke("run", 16, branch)
				view.HandleCount = count
				if err != nil || len(got) != 5 || got[0] != 0x0706050403020100 || got[1] != 0x0f0e0d0c0b0a0908 || got[2] != 42 || got[3] != bits || got[4] != 55 {
					t.Fatalf("fallback%v branch%d:%x,%v", fallback, branch, got, err)
				}
			}
		}
	}
}

func BenchmarkGCGeneralScalarArrayGet(b *testing.B) {
	for _, shape := range []struct {
		name string
		open bool
	}{{"old-open", true}, {"nursery-final", false}} {
		b.Run(shape.name, func(b *testing.B) {
			data := gcGeneralScalarArrayGetModule(0x7f, true, shape.open, false)
			b.Run("compile", func(b *testing.B) {
				b.ReportAllocs()
				size := 0
				for i := 0; i < b.N; i++ {
					c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
					if err != nil {
						b.Fatal(err)
					}
					size = c.CodeSize()
					c.Close()
				}
				b.ReportMetric(float64(size), "native-B")
			})
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{DisableCollection: !shape.open}})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("init", 17, 23); err != nil {
				b.Fatal(err)
			}
			if shape.open {
				if err := in.CollectGC(); err != nil {
					b.Fatal(err)
				}
			}
			for _, mode := range []string{"fast", "fallback", "mixed"} {
				b.Run(mode, func(b *testing.B) {
					view := in.gc.NativeView()
					count := view.HandleCount
					defer func() { view.HandleCount = count }()
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						view.HandleCount = count
						if mode == "fallback" || mode == "mixed" && i&1 != 0 {
							view.HandleCount = 0
						}
						got, err := in.Invoke("loop", 1000, uint64(i%17))
						if err != nil || len(got) != 1 || got[0] != 1000 {
							b.Fatalf("%x,%v", got, err)
						}
					}
					b.StopTimer()
					b.ReportMetric(1000, "guest-ops/op")
				})
			}
		})
	}
}

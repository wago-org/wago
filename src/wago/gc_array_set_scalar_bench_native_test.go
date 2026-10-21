//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
	"unsafe"
)

func BenchmarkGCGeneralScalarArraySet(b *testing.B) {
	for _, shape := range []struct {
		name string
		open bool
	}{{"old-open", true}, {"nursery-final", false}} {
		b.Run(shape.name, func(b *testing.B) {
			data := gcGeneralScalarArraySetModule(0x7f, shape.open, false)
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
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{}})
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
				if err := in.gc.ForcePromote(gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))); err != nil {
					b.Fatal(err)
				}
			}
			view := in.gc.NativeView()
			ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)) >> 1
			handles := unsafe.Slice((*byte)(offHeapPtr(view.Handles)), int(view.HandleCount)*gc.NativeHandleStride)
			space := handles[int(ref)*gc.NativeHandleStride+gc.NativeHandleSpaceOffset]
			wantSpace := gc.NativeSpaceNursery
			if shape.open {
				wantSpace = gc.NativeSpaceOld
			}
			if space != wantSpace {
				b.Fatalf("control%s space%d,want%d", shape.name, space, wantSpace)
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
						got, err := in.Invoke("loop", 1000, uint64(i%17), uint64(i))
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

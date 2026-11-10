//go:build amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func BenchmarkScalingControlBoundaries(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096} {
		for _, cold := range []bool{false, true} {
			b.Run(fmt.Sprintf("cold=%t/N=%d", cold, n), func(b *testing.B) {
				a := &x86.Asm{}
				f := fn{a: a, s: newStackWithCap(n + 8), ctrl: []ctrlFrame{{kind: cfBlock, controlSite: -1}}}
				types := make([]machineType, n)
				for i := range types {
					types[i] = mtI64
				}
				f.setDepthTypesWithGCRoots(types, nil)
				b.ReportAllocs()
				b.ResetTimer()
				for k := 0; k < b.N; k++ {
					r := wasm.NewReader([]byte{0x40})
					if err := f.opBlock(r, 0x02); err != nil {
						b.Fatal(err)
					}
					if cold {
						_ = f.frameBaseTypes(&f.ctrl[len(f.ctrl)-1])
					}
					if err := f.opEnd(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkScalingNestedControls(b *testing.B) {
	for _, n := range []int{8, 16, 32, 64, 128, 256, 512, 1024} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			types := make([]machineType, n)
			for i := range types {
				types[i] = mtI64
			}
			b.ReportAllocs()
			b.ResetTimer()
			for k := 0; k < b.N; k++ {
				f := fn{a: &x86.Asm{}, s: newStackWithCap(n + 8), ctrl: []ctrlFrame{{kind: cfBlock, controlSite: -1}}}
				f.setDepthTypesWithGCRoots(types, nil)
				for i := 0; i < n; i++ {
					if err := f.opBlock(wasm.NewReader([]byte{0x40}), 0x02); err != nil {
						b.Fatal(err)
					}
				}
				for i := 0; i < n; i++ {
					if err := f.opEnd(); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

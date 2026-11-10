//go:build linux && amd64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCLocalSnapshotAfterAssignment(t *testing.T) {
	for kind := 0; kind < 3; kind++ {
		for target := byte(0); target < 2; target++ {
			typ := []byte{0x5f, 1, 0x7f, 1}
			get := []byte{0xfb, 2, 0, 0}
			first := []byte{0x41, 3, 0xfb, 0, 0}
			second := []byte{0x41, 9, 0xfb, 0, 0}
			var extra []byte
			if kind == 1 {
				typ = []byte{0x5e, 0x7f, 1}
				get = []byte{0x41, 0, 0xfb, 0x0b, 0}
				first = []byte{0x41, 3, 0x41, 1, 0xfb, 6, 0}
				second = []byte{0x41, 9, 0x41, 1, 0xfb, 6, 0}
			}
			if kind == 2 {
				typ = []byte{0x50, 0, 0x5f, 1, 0x7f, 1}
				extra = []byte{0x4f, 1, 0, 0x5f, 1, 0x7f, 1}
				first = []byte{0x41, 3, 0xfb, 0, 1}
				second = []byte{0x41, 9, 0xfb, 0, 1}
			}
			body := []byte{0x20, target}
			if kind == 2 {
				body = append(body, 0xfb, 0x16, 1)
			} // materialize an owned cast result before the write
			body = append(body, 0x20, 1-target, 0x21, target)
			body = append(body, get...)
			body = append(body, 0x20, target)
			body = append(body, get...)
			body = append(body, 0x0b)
			wrapper := append(first, second...)
			wrapper = append(wrapper, 0x10, 0, 0x0b)
			types := [][]byte{typ}
			if extra != nil {
				types = append(types, extra)
			}
			funcType := byte(len(types))
			types = append(types, []byte{0x60, 2, 0x64, 0, 0x64, 0, 2, 0x7f, 0x7f}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}))
			data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec([]byte{funcType}, []byte{funcType + 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(wrapper))))
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("run")
				in.Close()
				want := []uint64{3, 9}
				if target == 1 {
					want = []uint64{9, 3}
				}
				if err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
					t.Errorf("kind%d target%d profile%d snapshot/current: %v,%v; want%v", kind, target, profile, got, err, want)
				}
			}
		}
	}
}

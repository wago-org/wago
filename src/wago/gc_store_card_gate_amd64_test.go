//go:build linux && amd64

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Model the survey's failure sequence with the smallest reference payload that
// crosses a 128-byte card: 33 four-byte references. Children contain distinct
// i32 values and are reachable only through the parent when collection runs.
func gcStoreCardGateFixture(array bool, first, last byte) []byte {
	fields := make([][]byte, 33)
	for i := range fields {
		fields[i] = []byte{0x6d, 0x01}
	}
	parentType := append([]byte{0x5f}, wasmtest.Vec(fields...)...)
	if array {
		parentType = []byte{0x5e, 0x6d, 0x01}
	}
	childType := append([]byte{0x5f}, wasmtest.Vec([]byte{0x7f, 0x00})...)
	init := []byte{0x00}
	if array {
		init = append(init, 0x41, 33, 0xfb, 0x07, 0x00)
	} else {
		init = append(init, 0xfb, 0x01, 0x00)
	}
	init = append(init, 0x24, 0x00, 0x0b)
	store := func(index, value byte) []byte {
		body := []byte{0x00, 0x23, 0x00}
		if array {
			body = append(body, 0x41, index)
		}
		body = append(body, 0x41, value, 0xfb, 0x00, 0x01)
		if array {
			body = append(body, 0xfb, 0x0e, 0x00)
		} else {
			body = append(body, 0xfb, 0x05, 0x00, index)
		}
		return append(body, 0x0b)
	}
	bodies := [][]byte{init, store(first, 11), store(last, 22)}
	for i, body := range bodies {
		bodies[i] = append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(parentType, childType, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("first", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("last", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCNativeReferenceStoreCardGate(t *testing.T) {
	for _, array := range []bool{false, true} {
		for _, large := range []bool{false, true} {
			for _, indices := range [][2]byte{{0, 1}, {0, 32}, {32, 0}} {
				for _, verify := range []bool{false, true} {
					t.Run(fmt.Sprintf("array=%t/large=%t/indices=%v/verify=%t", array, large, indices, verify), func(t *testing.T) {
						compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStoreCardGateFixture(array, indices[0], indices[1]))
						if err != nil {
							t.Fatal(err)
						}
						defer compiled.Close()
						cfg := GCConfig{VerifyAfterCollect: verify, StressBarriers: verify}
						if large {
							cfg.LargeObjectBytes = 128
						}
						in, err := Instantiate(compiled, InstantiateOptions{GC: cfg})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						invoke := func(name string) {
							t.Helper()
							if _, err := in.Invoke(name); err != nil {
								t.Fatal(err)
							}
						}
						invoke("init")
						parent := corergc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
						if err := in.gc.ForcePromote(parent); err != nil {
							t.Fatal(err)
						}
						invoke("first") // helper establishes remembered membership and its card
						if got := in.gc.RememberedCount(); got != 1 {
							t.Fatalf("remembered parents = %d, want 1", got)
						}
						invoke("last") // must honor coverage despite remembered membership
						read := func(index uint32) corergc.Ref {
							t.Helper()
							var value corergc.Value
							var err error
							if array {
								value, err = in.gc.ArrayGet(parent, index)
							} else {
								value, err = in.gc.StructGet(parent, index)
							}
							if err != nil || !value.Ref.IsObj() {
								t.Fatalf("parent[%d] = %+v, %v", index, value, err)
							}
							return value.Ref
						}
						children := []corergc.Ref{read(uint32(indices[0])), read(uint32(indices[1]))} // Go values are not collector roots
						if err := in.gc.CollectMinor(nil); err != nil {
							t.Fatal(err)
						}
						for i, index := range []uint32{uint32(indices[0]), uint32(indices[1])} {
							child := read(index)
							if child != children[i] {
								t.Fatalf("child identity changed: %v != %v", child, children[i])
							}
							value, err := in.gc.StructGet(child, 0)
							if err != nil || value.I32() != int32(11*(i+1)) {
								t.Fatalf("child[%d] after minor = %+v, %v; want %d", index, value, err, 11*(i+1))
							}
						}
					})
				}
			}
		}
	}
}

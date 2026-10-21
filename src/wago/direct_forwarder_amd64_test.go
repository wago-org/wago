//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func directForwarderRootsModule() []byte {
	return directForwarderRootsModuleFor(false)
}

func directForwarderRootsModuleFor(numericParam bool) []byte {
	bodies := [][]byte{wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0x0b})}
	forwardType := []byte{0x60, 1, 0x63, 0, 1, 0x63, 0}
	arg := byte(1)
	if numericParam {
		forwardType = []byte{0x60, 1, 0x7f, 1, 0x63, 0}
		bodies[0] = wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0x0b})
		arg = 0
	}
	for target := byte(0); target < 4; target++ {
		bodies = append(bodies, wasmtest.Code([]byte{0x20, 0, 0x10, target, 0x0b}))
	}
	// Keep two original roots, then the forwarded reference result, live across
	// allocating calls. The final target also retains its reference parameter.
	run := []byte{1, 3, 0x63, 0,
		0x20, 0, 0xfb, 0, 0, 0x21, 1,
		0x41, 0xe8, 7, 0xfb, 0, 0, 0x21, 2,
		0x20, arg, 0x10, 4, 0x21, 3,
		0x10, 6,
		0x20, 1, 0xfb, 2, 0, 0,
		0x20, 2, 0xfb, 2, 0, 0, 0x6a,
		0x20, 3, 0xfb, 2, 0, 0, 0x6a, 0x0b}
	bodies = append(bodies, append(wasmtest.ULEB(uint32(len(run))), run...),
		wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))
	functionTypes := [][]byte{{1}, {1}, {1}, {1}, {1}, {2}, {3}}
	if numericParam {
		// Tail-call modules select the existing numeric-parameter/reference-result
		// register ABI. This unrelated tail call enables that module-wide ABI.
		bodies = append(bodies, wasmtest.Code([]byte{0x12, 6, 0x0b}))
		functionTypes = append(functionTypes, []byte{3})
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1},
			forwardType,
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(functionTypes...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roots", 0, 5), wasmtest.ExportEntry("wrapper", 0, 4))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestDirectForwarderRoots(t *testing.T) {
	for _, numericParam := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("enabled%t/numericParam%t", enabled, numericParam), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("direct-forwarder", enabled), directForwarderRootsModuleFor(numericParam))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				artifacts := []*Compiled{c}
				if !numericParam {
					loaded := publicArtifactRoundTrip(t, c)
					defer loaded.Close()
					artifacts = append(artifacts, loaded)
				}
				// The public codec does not currently admit the tail-call feature
				// required by the reference-result register ABI. That variant tests
				// fresh native execution; the ordinary reference ABI tests both.
				for _, artifact := range artifacts {
					if status := artifact.GCNativeRootAdmission(); !status.Required || !status.Exact {
						t.Fatalf("exact roots required: %+v", status)
					}
					if artifact.Exports["wrapper"] != 4 {
						t.Fatalf("wrapper export identity changed: %v", artifact.Exports)
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
							for _, input := range []uint64{0, 17, 99} {
								got, err := in.Invoke("roots", input)
								if err != nil || len(got) != 1 || got[0] != 1000+2*input {
									t.Fatalf("roots(%d)=%v,%v", input, got, err)
								}
							}
						}()
					}
				}
			})
		}
	}
}

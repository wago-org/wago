//go:build arm64 && !tinygo

package wago

import (
	"bytes"
	"context"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func nativeImportVariantModule(pair bool) []byte {
	params := []wasm.ValType{wasm.I32}
	ownType := uint32(0)
	if pair {
		params = append(params, wasm.I32)
		ownType = 1
	}
	types := [][]byte{wasmtest.FuncType(params, []wasm.ValType{wasm.I32})}
	if pair {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("step")...), 0, 0)
	body := []byte{1, 2, 0x7f, 0x03, 0x40, 0x20, 2, 0x20, 1}
	if pair {
		body = append(body, 0x41, 7)
	}
	body = append(body, 0x10, 0, 0x20, 1, 0x41, 3, 0x6a)
	if pair {
		body = append(body, 0x41, 5)
	}
	body = append(body, 0x10, 1, 0x6a, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imp, imp)), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(ownType))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestNativeImportVariantSourceArtifactAndCalls(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling deliberately retains the general body")
	}
	t.Setenv("WAGO_ARM64_NATIVE_IMPORT_TARGET_CACHE", "1")
	for _, compact := range []string{"0", "1"} {
		t.Run("compact="+compact, func(t *testing.T) {
			t.Setenv("WAGO_COMPACT", compact)
			for _, pair := range []bool{false, true} {
				t.Run(fmt.Sprintf("pair=%v", pair), func(t *testing.T) {
					images := map[string][]byte{}
					for _, mode := range []string{"0", "1"} {
						t.Setenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT", mode)
						cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
						c, err := cfg.Compile(nativeImportVariantModule(pair))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						var image bytes.Buffer
						if _, err := c.WriteCodeTo(&image); err != nil {
							t.Fatal(err)
						}
						images[mode] = append([]byte(nil), image.Bytes()...)
						decoded := publicArtifactRoundTrip(t, c)
						defer decoded.Close()
						for index, code := range []*Compiled{c, decoded} {
							for _, native := range []bool{false, true} {
								rt := NewRuntime(WithRuntimeConfig(cfg))
								module, err := rt.Module(code)
								if err != nil {
									t.Fatal(err)
								}
								calls := 0
								var callback any = nativeLeafIncrement
								sig := FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}}
								if pair {
									callback = nativeLeafPolynomial
									sig.Params = append(sig.Params, ValI32)
								}
								if !native {
									if pair {
										callback = func(a, b int32) int32 { calls++; return a*b + a - b }
									} else {
										callback = func(v int32) int32 { calls++; return v + 1 }
									}
								}
								ref, err := rt.NewHostFuncRef(callback, sig)
								if err != nil {
									t.Fatal(err)
								}
								imports := NewImports().Function("env", "step", ref)
								in, err := rt.Instantiate(context.Background(), module, WithImports(imports))
								if err != nil {
									t.Fatal(err)
								}
								ref.Close()
								if admitted := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; admitted != (native && index == 0) {
									t.Fatalf("mode=%s native=%t artifact=%d admission=%v", mode, native, index, admitted)
								}
								wantCalls := 0
								for _, n := range []uint32{0, 1, 17, 129, 1024, 65536} {
									count := uint64(n)
									if count == 0 {
										count = 1
									}
									want := uint32(count * (count + 4))
									if pair {
										want = uint32(7*count*count - count)
									}
									got, err := in.Invoke("run", uint64(n))
									if err != nil || len(got) != 1 || got[0] != uint64(want) {
										t.Fatalf("mode=%s native=%v artifact=%d n=%d got=%v,%v want=%d", mode, native, index, n, got, err, want)
									}
									wantCalls += 2 * int(count)
								}
								if !native && calls != wantCalls {
									t.Fatalf("Go callback count=%d want=%d", calls, wantCalls)
								}
								in.Close()
								module.Close()
								rt.Close()
							}
						}
					}
					if bytes.Equal(images["0"], images["1"]) || len(images["1"]) <= len(images["0"]) {
						t.Fatal("guarded variant selector did not add a second body")
					}
				})
			}
		})
	}
}

func TestNativeImportVariantRespectsPolicies(t *testing.T) {
	for _, option := range []string{"native-leaf-host", "reg-abi"} {
		t.Run(option, func(t *testing.T) {
			var images [2][]byte
			for i, mode := range []string{"0", "1"} {
				t.Setenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT", mode)
				cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization(option, false)
				c, err := cfg.Compile(nativeImportVariantModule(false))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				var image bytes.Buffer
				if _, err := c.WriteCodeTo(&image); err != nil {
					t.Fatal(err)
				}
				images[i] = append([]byte(nil), image.Bytes()...)
				calls := 0
				in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.step", func(v int32) int32 { calls++; return v + 1 })})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				got, err := in.Invoke("run", 17)
				if err != nil || len(got) != 1 || got[0] != 357 || calls != 34 {
					t.Fatalf("got=%v err=%v calls=%d", got, err, calls)
				}
			}
			if !bytes.Equal(images[0], images[1]) {
				t.Fatal("disabled policy still emitted native variant")
			}
		})
	}
}

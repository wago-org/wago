//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

const rv64PublicGCBrOnCastAbstractHex = "0061736d0100000001a180808000076000017f5f0177005e780060016f0060017f017f60016b01640260016b0164010388808080000700030404040404048480808000016e000a07ca808080000604696e697400010a62725f6f6e5f6e756c6c00020962725f6f6e5f69333100030c62725f6f6e5f73747275637400040b62725f6f6e5f61727261790005096e756c6c2d64696666000609858080800001030001000ad9818080000784808080000041090bac80808000004100d06e260041014107fb1c260041024106fb00012600410341054103fb0602260041042000fb1a26000b908080800000024020002500d500417f0f0b41000b95808080000002646c20002500fb1801006e6c417f0f0bfb1e0bb2808080000002646b20002500fb1801006e6b417f0f0b02060205fb1801016b01fb1801006e02417e0f0b4100fb0d020f0bfb0301000b95808080000002646a20002500fb1801006e6a417f0f0bfb0f0b998080800000026b02646e20002500fb1803016e6b0b41000f0b41010f0b"
const rv64PublicGCBrOnCastFailAbstractHex = "0061736d0100000001a180808000076000017f5f0177005e780060016f0060017f017f60016b01640260016b0164010388808080000700030404040404048480808000016e000a07da808080000604696e697400010e62725f6f6e5f6e6f6e5f6e756c6c00020d62725f6f6e5f6e6f6e5f69333100031062725f6f6e5f6e6f6e5f73747275637400040f62725f6f6e5f6e6f6e5f61727261790005096e756c6c2d64696666000609858080800001030001000adb818080000784808080000041090bac80808000004100d06e260041014107fb1c260041024106fb00012600410341054103fb0602260041042000fb1a26000b92808080000002646e20002500d60041000f0b417f0f0b958080800000026e20002500fb1901006e6cfb1e0f0b417f0f0bb28080800000026e20002500fb1901006e6b02060205fb1801016b01fb1801006e02417e0f0b4100fb0d020f0bfb0301000f0b417f0f0b958080800000026e20002500fb1901006e6afb0f0f0b417f0f0b99808080000002646e026b20002500fb1903016e6b0b41010f0b41000f0b"

func rv64PublicGCBrOnCastAbstractBytes(t testing.TB, fail bool) []byte {
	t.Helper()
	source := rv64PublicGCBrOnCastAbstractHex
	if fail {
		source = rv64PublicGCBrOnCastFailAbstractHex
	}
	b, err := hex.DecodeString(source)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCBrOnCastAbstractCompile(t *testing.T, fail, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_MODE")
	name := "gc-br-on-cast-abstract.wago"
	if fail {
		name = "gc-br-on-cast-fail-abstract.wago"
	}
	path := filepath.Join(dir, name)
	if mode == "load" {
		if !artifact {
			t.Skip("compiler-free runner selected artifact case")
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = loaded.Close() })
		return loaded
	}
	original, err := rv64I31Config().Compile(rv64PublicGCBrOnCastAbstractBytes(t, fail))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	if !artifact && mode != "prepare" {
		return original
	}
	blob, err := original.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !artifact {
		return original
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	return loaded
}

func TestRV64PublicGCBrOnCastAbstractExecutionAndArtifact(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := map[bool]string{false: "success", true: "fail"}[fail]
		t.Run(name, func(t *testing.T) {
			data := rv64PublicGCBrOnCastAbstractBytes(t, fail)
			wantLen, wantDigest := 385, [sha256.Size]byte{0x44, 0x29, 0xdb, 0x75, 0x87, 0xba, 0x73, 0xad, 0xfc, 0x04, 0xc4, 0x4a, 0x23, 0x69, 0xba, 0xb3, 0x83, 0x43, 0xd7, 0xf5, 0x82, 0xd1, 0x74, 0x53, 0x72, 0x94, 0x0b, 0xa9, 0x6c, 0x04, 0xa2, 0x63}
			if fail {
				wantLen, wantDigest = 403, [sha256.Size]byte{0xa1, 0xd3, 0x39, 0xdf, 0xb4, 0xed, 0x4a, 0xa3, 0x08, 0xf1, 0xa9, 0xee, 0xb8, 0x29, 0x3f, 0x04, 0x75, 0xd4, 0x27, 0xb0, 0xd7, 0xae, 0xa7, 0xe8, 0xb9, 0x91, 0x59, 0xc0, 0x54, 0xfc, 0x68, 0x15}
			}
			if len(data) != wantLen || sha256.Sum256(data) != wantDigest {
				t.Fatal("abstract branch-cast source identity changed")
			}
			for _, artifact := range []bool{false, true} {
				t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
					compiled := rv64PublicGCBrOnCastAbstractCompile(t, fail, artifact)
					view := compiled.executionView()
					if !rv64ArtifactContractValid(view) || railshotValidateCompiled(view) != nil {
						t.Fatalf("invalid branch-cast execution contract: %v", railshotValidateCompiled(view))
					}
					if params, results, err := view.maxCallSlots(); err != nil || params != 4 || results != 1 {
						t.Fatalf("branch-cast call capacity=%d/%d,%v want 4/1", params, results, err)
					}
					for _, profile := range []struct {
						name string
						gc   GCConfig
					}{{name: "default-gc"}, {name: "stress-gc", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
						t.Run(profile.name, func(t *testing.T) {
							rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
							defer rt.Close()
							mod, err := rt.Module(compiled)
							if err != nil {
								t.Fatal(err)
							}
							in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(profile.gc))
							mod.Close()
							if err != nil {
								t.Fatal(err)
							}
							state := in.existingGCRefTestTableState()
							if in.gc == nil || state == nil || state.Count != 10 || state.Conversion == nil {
								t.Fatalf("collector/branch-cast state=%p/%+v", in.gc, state)
							}
							foreign, err := in.NewExternRef("branch-cast-foreign")
							if err != nil {
								t.Fatal(err)
							}
							if got, err := in.Invoke("init", ValueExternRef(foreign).Bits()); err != nil || len(got) != 0 {
								t.Fatalf("init=%v,%v", got, err)
							}
							prefix := "br_on_"
							if fail {
								prefix = "br_on_non_"
							}
							cases := map[string][]uint32{
								prefix + "null":   {0, ^uint32(0), ^uint32(0), ^uint32(0), ^uint32(0)},
								prefix + "i31":    {^uint32(0), 7, ^uint32(0), ^uint32(0), ^uint32(0)},
								prefix + "struct": {^uint32(0), ^uint32(0), 6, ^uint32(0), ^uint32(0)},
								prefix + "array":  {^uint32(0), ^uint32(0), ^uint32(0), 3, ^uint32(0)},
								"null-diff":       {1, 0, 1, 0, 0},
							}
							for field, wants := range cases {
								for index, want := range wants {
									arg := uint64(0xfeedface)<<32 | uint64(index)
									got, err := in.Invoke(field, arg)
									if err != nil || len(got) != 1 || uint32(got[0]) != want {
										t.Fatalf("%s(%#x)=%v,%v want %d", field, arg, got, err, want)
									}
								}
								got, err := in.Invoke(field, uint64(0xfeedface)<<32|10)
								var trap *TrapError
								if got != nil || !errors.As(err, &trap) || trap.Code != TrapTableOutOfBounds {
									t.Fatalf("%s oob=%v,%v", field, got, err)
								}
							}
							if err := in.gc.CollectFull(nil); err != nil {
								t.Fatal(err)
							}
							if live := in.gc.Stats().LiveObjects; live != 2 {
								t.Fatalf("live objects=%d want 2", live)
							}
							if err := in.Close(); err != nil {
								t.Fatal(err)
							}
							if !state.Conversion.closed || state.Conversion.count != 0 {
								t.Fatalf("closed conversion state=%t/%d", state.Conversion.closed, state.Conversion.count)
							}
						})
					}
				})
			}
		})
	}
}

func TestRV64PublicGCBrOnCastAbstractAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "fail"}[fail], func(t *testing.T) {
			base := rv64PublicGCBrOnCastAbstractCompile(t, fail, false).executionView()
			export := "br_on_array"
			if fail {
				export = "br_on_non_array"
			}
			for name, mutate := range map[string]func(*Compiled){
				"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
				"entry":      func(v *Compiled) { v.Entry[6]++; v.InternalEntry[6]++ },
				"function":   func(v *Compiled) { v.Funcs[5].Results = nil },
				"type":       func(v *Compiled) { v.Types[2].Array.Mutable = true },
				"type-key":   func(v *Compiled) { v.FuncTypeID[3] ^= 1 },
				"descriptor": func(v *Compiled) { v.GCTypeDescs[1].Align = 1 },
				"value-type": func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Abstract = AbstractHeapEq },
				"table":      func(v *Compiled) { v.TableSize++ },
				"export": func(v *Compiled) {
					delete(v.Exports, export)
					v.Exports["other"] = 5
				},
				"feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMultiValue },
			} {
				t.Run(name, func(t *testing.T) {
					candidate := rv64ReferenceStartCopy(base)
					mutate(candidate)
					if rv64ArtifactContractValid(candidate) {
						t.Fatal("hostile retained mutation kept abstract branch-cast witness")
					}
					if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
						t.Fatal("hostile retained abstract branch-cast mutation admitted", err)
					}
				})
			}
			t.Run("array-product", func(t *testing.T) {
				candidate := rv64ReferenceStartCopy(base)
				candidate.ensureCodeCache()
				previous := candidate.codeCache.gcArrayProduct
				candidate.codeCache.gcArrayProduct = stagedGCArrayProductGeneric
				t.Cleanup(func() { candidate.codeCache.gcArrayProduct = previous })
				if rv64ArtifactContractValid(candidate) {
					t.Fatal("hostile retained array product kept abstract branch-cast witness")
				}
				if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("hostile retained abstract branch-cast array product admitted", err)
				}
			})
		})
	}
}

func TestRV64PublicGCBrOnCastAbstractCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for _, fail := range []bool{false, true} {
		c, err := rv64I31Config().Compile(rv64PublicGCBrOnCastAbstractBytes(t, fail))
		if c != nil {
			_ = c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free branch-cast path used source compilation", err)
		}
	}
}

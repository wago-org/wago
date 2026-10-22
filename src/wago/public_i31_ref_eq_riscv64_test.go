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

const rv64OfficialObjectRefEqHex = "0061736d0100000001af808080000950005f0050005f017f005e78005001005f005001005f005001015f017f005001015f017f0060000060027f7f017f038380808000020708048480808000016d0014078d808080000204696e6974000002657100010adc8080800002c680808000004100d06d26004101d06c260041024107fb1c260041034107fb1c260041044108fb1c26004105fb010026004106fb0100260041074100fb0702260041084100fb070226000b8b80808000002000250020012500d30b"

func rv64I31RefEqPilotCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_I31_REF_EQ_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_I31_REF_EQ_ARTIFACT_MODE")
	path := filepath.Join(dir, "i31-ref-eq-pilot.wago")
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
	original, err := rv64I31Config().Compile(stagedGCI31RefEqPilotBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original
	if artifact || mode == "prepare" {
		blob, err := original.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if mode == "prepare" {
			if err = os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, blob, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if artifact {
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = loaded.Close() })
			c = loaded
		}
	}
	return c
}

func TestRV64PublicI31RefEqPilotExecutionAndArtifact(t *testing.T) {
	data := stagedGCI31RefEqPilotBytes(t)
	wantSource := [sha256.Size]byte{0xd4, 0x1b, 0x88, 0xea, 0xab, 0x33, 0x8e, 0x5b, 0x01, 0xe6, 0x7e, 0xbf, 0x43, 0x52, 0xce, 0x12, 0x6d, 0xfd, 0xa5, 0x0d, 0x26, 0xaa, 0xb2, 0x3c, 0x76, 0x1f, 0xd5, 0x6c, 0x3a, 0x29, 0xd5, 0xaf}
	if len(data) != 128 || sha256.Sum256(data) != wantSource {
		t.Fatal("i31 ref.eq pilot source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64I31RefEqPilotCompile(t, artifact)
			view := c.executionView()
			wantProduct, wantStaged := stagedGCI31ProductRefEqPilot, CoreFeatureTypedFunctionReferences|CoreFeatureGC
			if artifact {
				wantProduct, wantStaged = stagedGCI31ProductCore, CoreFeatureGC
			}
			if !rv64I31RefEqPilot(view) || !rv64I31Core(view) || railshotValidateCompiled(view) != nil || c.stagedGCI31Product() != wantProduct || c.stagedFeatures() != wantStaged {
				t.Fatalf("missing exact retained i31 ref.eq witness: product=%v staged=%v required=%v code=%d/%x entries=%v/%v table=%v/%v/%d/%d/%v", c.stagedGCI31Product(), c.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.TableType, view.TableHasValueType, view.TableSize, view.TableMax, view.TableHasMax)
			}
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			if in.gc != nil {
				t.Fatal("collector-free i31 ref.eq pilot provisioned a collector")
			}
			invoke := func(name string, want []uint64, args ...uint64) {
				t.Helper()
				got, err := in.Invoke(name, args...)
				if err != nil || !slices.Equal(got, want) {
					t.Fatalf("%s%v=%v err=%v want=%v", name, args, got, err, want)
				}
			}
			invoke("init", nil)
			invoke("eq", []uint64{1}, 0xdeadbeef00000000, 0xcafebabe00000001)
			invoke("eq", []uint64{1}, 0x1111111100000002, 0x2222222200000003)
			invoke("eq", []uint64{0}, 0x3333333300000002, 0x4444444400000004)
			invoke("eq", []uint64{0}, 0x5555555500000000, 0x6666666600000002)
			_ = c.Close()
			invoke("eq", []uint64{1}, 2, 3)
			if _, err := in.Invoke("eq", I32(5), I32(0)); err == nil || !strings.Contains(err.Error(), "table") {
				t.Fatalf("out-of-bounds ref.eq table read did not trap: %v", err)
			}
		})
	}
}

func TestRV64PublicI31RefEqPilotCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_I31_REF_EQ_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(stagedGCI31RefEqPilotBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free i31 ref.eq path used source compilation", err)
	}
}

func rv64OfficialGCRefEqCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_REF_EQ_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_REF_EQ_ARTIFACT_MODE")
	path := filepath.Join(dir, "official-gc-ref-eq.wago")
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
	data, err := hex.DecodeString(rv64OfficialObjectRefEqHex)
	if err != nil {
		t.Fatal(err)
	}
	original, err := rv64I31Config().Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original
	if artifact || mode == "prepare" {
		blob, err := original.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if mode == "prepare" {
			if err = os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, blob, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if artifact {
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = loaded.Close() })
			c = loaded
		}
	}
	return c
}

func TestRV64PublicOfficialGCRefEqExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64OfficialGCRefEqCompile(t, artifact)
			view := compiled.executionView()
			if !rv64OfficialGCRefEq(view) || !rv64OfficialGCRefEqExecutionCode(view) || railshotValidateCompiled(view) != nil {
				t.Fatalf("missing exact retained allocating gc/ref.eq witness: product=%v array=%v staged=%v required=%v code=%d/%x", view.stagedGCStructProduct(), view.stagedGCArrayProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code))
			}
			for _, profile := range []struct {
				name                      string
				config                    GCConfig
				requireCollectsOnAllocate bool
			}{
				{name: "default-gc"},
				{name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}, requireCollectsOnAllocate: true},
			} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
					defer rt.Close()
					mod, err := rt.Module(compiled)
					if err != nil {
						t.Fatal(err)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(profile.config))
					mod.Close()
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if in.gc == nil {
						t.Fatal("allocating official ref_eq module has no collector")
					}
					if profile.requireCollectsOnAllocate && !in.gc.CollectsOnAllocation() {
						t.Fatal("stress collector does not collect on allocation")
					}
					if _, err = in.Invoke("init"); err != nil {
						t.Fatal(err)
					}
					if err = in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					for _, tc := range []struct {
						a, b uint64
						want uint64
					}{
						{0, 1, 1}, {2, 3, 1}, {2, 4, 0}, {5, 5, 1}, {5, 6, 0}, {7, 7, 1}, {7, 8, 0}, {5, 7, 0},
					} {
						got, err := in.Invoke("eq", tc.a, tc.b)
						if err != nil || len(got) != 1 || got[0] != tc.want {
							t.Fatalf("eq(%d,%d)=%v err=%v want=%d", tc.a, tc.b, got, err, tc.want)
						}
					}
				})
			}
		})
	}
}

func TestRV64PublicOfficialGCRefEqCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_REF_EQ_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	data, err := hex.DecodeString(rv64OfficialObjectRefEqHex)
	if err != nil {
		t.Fatal(err)
	}
	c, err := rv64I31Config().Compile(data)
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free allocating gc/ref.eq path used source compilation", err)
	}
}

func TestRV64PublicOfficialGCRefEqAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_REF_EQ_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64OfficialGCRefEqCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[1], v.InternalEntry[1] = 2408, 2408 },
		"function":   func(v *Compiled) { v.Funcs[1].Params[0] = ValI64 },
		"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[2].Elem = 0 },
		"supertype":  func(v *Compiled) { v.Types[3].Supers[0] = 1 },
		"export": func(v *Compiled) {
			delete(v.Exports, "eq")
			v.Exports["ne"] = 1
		},
		"zero-export": func(v *Compiled) {
			delete(v.Exports, "init")
			v.Exports["renamed"] = 0
		},
		"table-size": func(v *Compiled) { v.TableSize = 19 },
		"table-type": func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Abstract = AbstractHeapAny },
		"missing-gc": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"extra-feature": func(v *Compiled) {
			v.requiredFeatures |= CoreFeatureTypedFunctionReferences
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64OfficialGCRefEq(candidate) {
				t.Fatal("hostile retained mutation retained the allocating gc/ref.eq witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained allocating gc/ref.eq mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicI31RefEqPilotAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_I31_REF_EQ_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64I31RefEqPilotCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[1], v.InternalEntry[1] = 1496, 1496 },
		"function":   func(v *Compiled) { v.Funcs[1].Params[0] = ValI64 },
		"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[1].Final = false },
		"export": func(v *Compiled) {
			delete(v.Exports, "eq")
			v.Exports["ne"] = 1
		},
		"zero-export": func(v *Compiled) {
			delete(v.Exports, "init")
			v.Exports["renamed"] = 0
		},
		"table-size": func(v *Compiled) { v.TableSize = 4 },
		"table-type": func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Abstract = AbstractHeapAny },
		"missing-gc": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"extra-feature": func(v *Compiled) {
			v.requiredFeatures |= CoreFeatureTypedFunctionReferences
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64I31RefEqPilot(candidate) || rv64I31Core(candidate) {
				t.Fatal("hostile retained mutation retained the i31 ref.eq witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained i31 ref.eq mutation admitted", err)
			}
		})
	}
}

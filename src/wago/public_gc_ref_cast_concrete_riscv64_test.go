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
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64PublicGCRefCastConcreteHex = "0061736d0100000001bf808080000950005f005001005f017f005001005f017f005001015f027f007f005001025f027f007f005001005f027f007f005001005f005001065f027f007f0060000003848080800003080808048480808000016b00140799808080000208746573742d73756200010a746573742d63616e6f6e00020afa8280800003ba80808000004100fb01002600410afb010026004101fb01012600410bfb010226004102fb01032600410cfb010426004103fb010526004104fb010726000bbc81808000001000d06bfb17001a41002500fb17001a41012500fb17001a41022500fb17001a41032500fb17001a41042500fb17001ad06bfb17001a41012500fb17011a41022500fb17011ad06bfb17001a41022500fb17031ad06bfb17001a41032500fb17051a41042500fb17071a41002500fb16001a41012500fb16001a41022500fb16001a41032500fb16001a41042500fb16001a41012500fb16011a41022500fb16011a41022500fb16031a41032500fb16051a41042500fb16071a0bf48080800000100041002500fb16001a41012500fb16001a41022500fb16001a41032500fb16001a41042500fb16001a410a2500fb16001a410b2500fb16001a410c2500fb16001a41012500fb16021a41022500fb16021a410b2500fb16011a410c2500fb16011a41022500fb16041a410c2500fb16031a0b"

func rv64PublicGCRefCastConcreteBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCRefCastConcreteHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCRefCastConcreteCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-ref-cast-concrete.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCRefCastConcreteBytes(t))
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

func TestRV64PublicGCRefCastConcreteExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCRefCastConcreteBytes(t)
	if len(data) != 512 || sha256.Sum256(data) != [sha256.Size]byte{0x65, 0xf1, 0xf3, 0x3b, 0x33, 0x5c, 0xa6, 0x2d, 0x90, 0xad, 0x08, 0x9a, 0x74, 0xf8, 0xa2, 0x9e, 0xa3, 0x16, 0x3f, 0x9a, 0x3a, 0x2f, 0x53, 0x09, 0x6b, 0xde, 0xac, 0x9e, 0x4b, 0x86, 0xf4, 0xa6} {
		t.Fatal("concrete gc/ref_cast source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCRefCastConcreteCompile(t, artifact)
			view := compiled.executionView()
			if !rv64ArtifactContractValid(view) || railshotValidateCompiled(view) != nil {
				t.Fatalf("missing exact concrete gc/ref_cast witness: exact=%t validation=%v product=%v staged=%v required=%v code=%d/%x entries=%v/%v flags=ctx:%t tablectx:%t tableref:%t regoff:%t table64:%v hdr:%t exports:%t desc:%t escape:%t ref-fn:%t ref-call:%t ref-control:%t tail:%t funcs=%+v ids=%v types=%+v descs=%+v value=%+v", rv64GCRefCastConcrete(view), railshotValidateCompiled(view), view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.nativeContextTrapABI, view.nativeTableContextABI, view.nativeTableReferenceABI, view.registerABIDisabled, view.nativeTable64ABI, view.needsFuncRefContextHeader, view.hasTableExportMetadata, view.NeedsFuncRefDescs, view.dynamicFuncrefEscape, view.nativeReferenceFunctionABI, view.nativeReferenceCallABI, view.nativeReferenceControlABI, rv64HasReferenceTail(view), view.Funcs, view.FuncTypeID, view.Types, view.GCTypeDescs, view.ValueTypes)
			}
			if params, results, err := view.maxCallSlots(); err != nil || params != 0 || results != 0 {
				t.Fatalf("concrete gc/ref_cast call capacity=%d/%d,%v, want 0/0", params, results, err)
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
					defer in.Close()
					if in.gc == nil {
						t.Fatal("table execution has no collector")
					}
					for iteration := 0; iteration < 5; iteration++ {
						for _, name := range []string{"test-sub", "test-canon"} {
							if got, err := in.Invoke(name); err != nil || len(got) != 0 {
								t.Fatalf("%s iteration %d=%v,%v", name, iteration, got, err)
							}
						}
					}
					// Exercise the same helper state with a hostile null/type mismatch.
					// The rejected cast must not modify the rooted table word; restoring
					// the original word must leave the official function successful.
					roots := rv64PublicGCTableRoots(t, in, 20)
					zero := roots[0].GetRef()
					roots[0].SetRef(corergc.Null())
					typeZero, ok := in.gcDomainType(0)
					if !ok {
						t.Fatal("concrete cast type 0 has no Runtime identity")
					}
					if value, err := in.gc.RefCast(roots[0].GetRef(), corergc.RefTestTarget{Kind: corergc.RefTestDefined, Type: typeZero}); !errors.Is(err, corergc.ErrCastFailure) {
						t.Fatalf("null unexpectedly cast to non-null type 0 as %#x: %v", value, err)
					}
					if !roots[0].GetRef().IsNull() {
						t.Fatal("rejected null cast changed the table root")
					}
					roots[0].SetRef(zero)
					if got, err := in.Invoke("test-sub"); err != nil || len(got) != 0 {
						t.Fatalf("test-sub after trap restore=%v,%v", got, err)
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					for _, name := range []string{"test-sub", "test-canon"} {
						if got, err := in.Invoke(name); err != nil || len(got) != 0 {
							t.Fatalf("%s after GC=%v,%v", name, got, err)
						}
					}
				})
			}
		})
	}
}

func TestRV64PublicGCRefCastConcreteAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCRefCastConcreteCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[2]++; v.InternalEntry[2]++ },
		"function":   func(v *Compiled) { v.Funcs[1].Results = []ValType{ValI32} },
		"type-super": func(v *Compiled) { v.Types[1].Supers[0] = 6 },
		"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[3].Super = 2 },
		"table-type": func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Abstract = AbstractHeapEq },
		"table-size": func(v *Compiled) { v.TableSize++ },
		"export":     func(v *Compiled) { delete(v.Exports, "test-sub"); v.Exports["other"] = 1 },
		"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if rv64ArtifactContractValid(candidate) {
				t.Fatal("hostile retained mutation kept concrete gc/ref_cast witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained concrete gc/ref_cast mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCRefCastConcreteCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCRefCastConcreteBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free concrete gc/ref_cast path used source compilation", err)
	}
}

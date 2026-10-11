//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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

const rv64PublicGCBrOnCastConcreteHex = "0061736d0100000001bf808080000950005f005001005f017f005001005f017f005001015f027f007f005001025f027f007f005001005f027f007f005001005f005001065f027f007f0060000003848080800003080808048480808000016b00140799808080000208746573742d73756200010a746573742d63616e6f6e00020afe8480800003ba80808000004100fb01002600410afb010626004101fb01012600410bfb010226004102fb01032600410cfb010426004103fb010526004104fb010726000be782808000001000026b026bd06bfb1801006b000b1a026b41002500fb1801006b000b1a026b41012500fb1801006b000b1a026b41022500fb1801006b000b1a026b41032500fb1801006b000b1a026b41042500fb1801006b000b1a026bd06bfb1801006b010b1a026b41012500fb1801006b010b1a026b41022500fb1801006b010b1a026bd06bfb1801006b030b1a026b41022500fb1801006b030b1a026bd06bfb1801006b050b1a026b41032500fb1801006b050b1a026bd06bfb1801006b070b1a026b41042500fb1801006b070b1a41002500fb1801006e0141032500fb1801006e0141042500fb1801006e0141002500fb1801006e0341012500fb1801006e0341032500fb1801006e0341042500fb1801006e0341002500fb1801006e0541012500fb1801006e0541022500fb1801006e0541042500fb1801006e0541002500fb1801006e0741012500fb1801006e0741022500fb1801006e0741032500fb1801006e070f0b000bcd818080000010000240026b41002500fb1801006b060b1a026b41012500fb1801006b060b1a026b41022500fb1801006b060b1a026b41032500fb1801006b060b1a026b41042500fb1801006b060b1a026b410a2500fb1801006b000b1a026b410b2500fb1801006b000b1a026b410c2500fb1801006b000b1a026b41012500fb1801006b020b1a026b41022500fb1801006b020b1a026b410b2500fb1801006b010b1a026b410c2500fb1801006b010b1a026b41022500fb1801006b040b1a026b410c2500fb1801006b030b1a0f0b000b"

const rv64PublicGCBrOnCastFailConcreteHex = "0061736d0100000001bf808080000950005f005001005f017f005001005f017f005001015f027f007f005001025f027f007f005001005f027f007f005001005f005001065f027f007f0060000003848080800003080808048480808000016b00140799808080000208746573742d73756200010a746573742d63616e6f6e00020ae68580800003ba80808000004100fb01002600410afb010026004101fb01012600410bfb010226004102fb01032600410cfb010426004103fb010526004104fb010726000b8784808000001000026bd06bfb1903006b0041002500fb1903006b0041012500fb1903006b0041022500fb1903006b0041032500fb1903006b0041042500fb1903006b0041002500fb1901006b0041012500fb1901006b0041022500fb1901006b0041032500fb1901006b0041042500fb1901006b00d06bfb1903006b0141012500fb1903006b0141022500fb1903006b0141012500fb1901006b0141022500fb1901006b01d06bfb1903006b0341022500fb1903006b0341022500fb1901006b03d06bfb1903006b0541032500fb1903006b0541032500fb1901006b05d06bfb1903006b0741042500fb1903006b0741042500fb1901006b07026bd06bfb1901006b000b1a026bd06bfb1901006b010b1a026b41002500fb1901006b010b1a026b41032500fb1901006b010b1a026b41042500fb1901006b010b1a026bd06bfb1901006b030b1a026b41002500fb1901006b030b1a026b41012500fb1901006b030b1a026b41032500fb1901006b030b1a026b41042500fb1901006b030b1a026bd06bfb1901006b050b1a026b41002500fb1901006b050b1a026b41012500fb1901006b050b1a026b41022500fb1901006b050b1a026b41042500fb1901006b050b1a026bd06bfb1901006b070b1a026b41002500fb1901006b070b1a026b41012500fb1901006b070b1a026b41022500fb1901006b070b1a026b41032500fb1901006b070b1a0f0b000b9581808000001000026b41002500fb1901006b0041012500fb1901006b0041022500fb1901006b0041032500fb1901006b0041042500fb1901006b00410a2500fb1901006b00410b2500fb1901006b00410c2500fb1901006b0041012500fb1901006b0241022500fb1901006b02410b2500fb1901006b01410c2500fb1901006b0141022500fb1901006b04410c2500fb1901006b030f0b000b"

func rv64PublicGCBrOnCastConcreteBytes(t testing.TB, fail bool) []byte {
	t.Helper()
	source := rv64PublicGCBrOnCastConcreteHex
	if fail {
		source = rv64PublicGCBrOnCastFailConcreteHex
	}
	b, err := hex.DecodeString(source)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64PublicGCBrOnCastConcreteExecutionAndArtifact(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "fail"}[fail], func(t *testing.T) {
			data := rv64PublicGCBrOnCastConcreteBytes(t, fail)
			wantDigest := [sha256.Size]byte{0x4e, 0xab, 0xad, 0xd9, 0x8e, 0x55, 0xbc, 0x0c, 0x83, 0x60, 0x0f, 0x07, 0x2a, 0x16, 0xba, 0xb7, 0x5e, 0x3d, 0x74, 0x17, 0x0d, 0x2f, 0x24, 0xbb, 0x6b, 0xda, 0xe4, 0xac, 0xf0, 0xb5, 0x49, 0x1b}
			if fail {
				wantDigest = [sha256.Size]byte{0x88, 0x89, 0x41, 0x27, 0xfa, 0xbe, 0x14, 0xa4, 0x2d, 0xc6, 0xd5, 0xaf, 0x44, 0x02, 0x73, 0x18, 0x07, 0x8b, 0x70, 0x2b, 0xcc, 0x67, 0xfc, 0x34, 0xe7, 0x52, 0xd1, 0x45, 0xa8, 0x16, 0x43, 0x12}
			}
			if sha256.Sum256(data) != wantDigest {
				t.Fatal("concrete branch-cast source identity changed")
			}
			dir, mode := os.Getenv("WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_MODE")
			name := "gc-br-on-cast-concrete.wago"
			if fail {
				name = "gc-br-on-cast-fail-concrete.wago"
			}
			path := filepath.Join(dir, name)
			var compiledSet []*Compiled
			if mode == "load" {
				blob, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if err != nil {
					t.Fatal(err)
				}
				compiledSet = []*Compiled{loaded}
			} else {
				original, err := rv64I31Config().Compile(data)
				if err != nil {
					t.Fatal(err)
				}
				blob, err := original.MarshalBinary()
				if err != nil {
					original.Close()
					t.Fatal(err)
				}
				if mode == "prepare" {
					if err = os.MkdirAll(dir, 0o700); err != nil {
						original.Close()
						t.Fatal(err)
					}
					if err = os.WriteFile(path, blob, 0o600); err != nil {
						original.Close()
						t.Fatal(err)
					}
				}
				loaded, err := LoadTrustedArtifact(blob)
				if err != nil {
					original.Close()
					t.Fatal(err)
				}
				compiledSet = []*Compiled{original, loaded}
			}
			for _, compiled := range compiledSet {
				defer compiled.Close()
				view := compiled.executionView()
				exact, wantProduct := rv64GCBrOnCastConcrete(view), stagedGCStructBrOnCastConcrete
				if fail {
					exact, wantProduct = rv64GCBrOnCastFailConcrete(view), stagedGCStructBrOnCastFailConcrete
				}
				if !exact || view.stagedGCStructProduct() != wantProduct || railshotValidateCompiled(view) != nil {
					t.Fatalf("missing exact concrete branch-cast witness: exact=%t product=%v validation=%v", exact, view.stagedGCStructProduct(), railshotValidateCompiled(view))
				}
				if params, results, err := view.maxCallSlots(); err != nil || params != 4 || results != 1 {
					t.Fatalf("call capacity=%d/%d,%v want 4/1", params, results, err)
				}
				for _, gcConfig := range []GCConfig{{}, {CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}} {
					rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
					mod, err := rt.Module(compiled)
					if err != nil {
						rt.Close()
						t.Fatal(err)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(gcConfig))
					mod.Close()
					if err != nil {
						rt.Close()
						t.Fatal(err)
					}
					for _, export := range []string{"test-sub", "test-canon"} {
						if got, err := in.Invoke(export); err != nil || len(got) != 0 {
							t.Fatalf("%s=%v,%v", export, got, err)
						}
					}
					if err := in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 8 {
						t.Fatalf("live objects=%d want 8 rooted table entries", live)
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					rt.Close()
				}
			}
		})
	}
}

func TestRV64PublicGCBrOnCastConcreteAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "fail"}[fail], func(t *testing.T) {
			compiled, err := rv64I31Config().Compile(rv64PublicGCBrOnCastConcreteBytes(t, fail))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			base := compiled.executionView()
			exact := rv64GCBrOnCastConcrete
			if fail {
				exact = rv64GCBrOnCastFailConcrete
			}
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
					candidate := cloneCompiledMetadata(base)
					candidate.validateMemo = &validateMemo{}
					mutate(candidate)
					if exact(candidate) {
						t.Fatal("hostile retained mutation kept concrete branch-cast witness")
					}
					if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
						t.Fatal("hostile retained concrete branch-cast mutation admitted", err)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCBrOnCastConcreteCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for _, fail := range []bool{false, true} {
		c, err := rv64I31Config().Compile(rv64PublicGCBrOnCastConcreteBytes(t, fail))
		if c != nil {
			_ = c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free concrete branch-cast path used source compilation", err)
		}
	}
}

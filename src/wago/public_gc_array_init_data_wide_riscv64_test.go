//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

const rv64PublicGCArrayInitDataWideHex = "0061736d01000000018a80808000035e7f015e7e01600000038d808080000c02020202020202020202020207bd808080000c0266300000026631000102663200020266330003026634000402663900050267300006026731000702673400080267370009026738000a026739000b0c8180808000070a89828080000c9180808000004101fb0700410041004101fb1200000b9180808000004101fb0700410041004101fb1200010b9180808000004101fb0700410041004101fb1200020b9180808000004101fb0700410041004101fb1200030b9180808000004101fb0700410041004101fb1200040b9180808000004101fb0700410041064101fb1200060b9180808000004101fb0701410041004101fb1201000b9180808000004101fb0701410041004101fb1201010b9180808000004101fb0701410041004101fb1201040b9180808000004101fb0701410041004101fb1201050b9180808000004101fb0701410041004101fb1201060b9180808000004101fb0701410041024101fb1201060b0ba9808080000701000101310102313201033132330104313233340107313233343536370109313233343536373839"

func rv64PublicGCArrayInitDataWideBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayInitDataWideHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayInitDataWideCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-array-init-data-wide.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCArrayInitDataWideBytes(t))
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

func TestRV64PublicGCArrayInitDataWideExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayInitDataWideBytes(t)
	wantSource := [sha256.Size]byte{0x05, 0x82, 0x7a, 0x01, 0xce, 0xc2, 0xe9, 0xf3, 0x62, 0x3e, 0x9d, 0x00, 0xb5, 0x4a, 0xff, 0x25, 0x8b, 0xbc, 0x7b, 0x49, 0x7f, 0x47, 0xb7, 0x6f, 0xfd, 0x31, 0x45, 0x2b, 0xbc, 0xb9, 0xb3, 0x1f}
	if len(data) != 435 || sha256.Sum256(data) != wantSource {
		t.Fatal("wide array.init_data source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayInitDataWideCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64GCArrayInitData(view) || !rv64GCArrayInitDataWideMetadata(view) || !rv64GCArrayInitDataExecutionCode(view) || err != nil {
				t.Fatalf("missing exact wide array.init_data witness: validation=%v product=%v code=%d/%x entries=%v/%v", err, view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			if params, results, err := railshotImportedCallSlots(view, 0, 0); err != nil || params != 6 || results != 0 {
				t.Fatalf("wide array.init_data call arena=%d/%d,%v, want 6/0", params, results, err)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 224, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
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
					if len(in.serArgs) < 6*8 {
						t.Fatalf("wide array.init_data argument arena=%d bytes, want at least 48", len(in.serArgs))
					}
					wantLengths := []uint32{0, 1, 2, 3, 4, 7, 9}
					if len(in.passiveDataDesc) != len(wantLengths)*16 {
						t.Fatalf("passive descriptor bytes=%d, want %d", len(in.passiveDataDesc), len(wantLengths)*16)
					}
					for i, want := range wantLengths {
						if got := binary.LittleEndian.Uint32(in.passiveDataDesc[i*16+8:]); got != want {
							t.Fatalf("passive descriptor %d length=%d, want %d", i, got, want)
						}
					}
					for _, name := range []string{"f0", "f1", "f2", "f3", "f9", "g0", "g1", "g4", "g7", "g9"} {
						if _, err = in.Invoke(name); err == nil {
							t.Fatalf("trapping %s returned normally", name)
						}
					}
					for _, name := range []string{"f4", "g8"} {
						if got, err := in.Invoke(name); err != nil || len(got) != 0 {
							t.Fatalf("successful %s=%v,%v", name, got, err)
						}
					}
					if err = in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 0 {
						t.Fatalf("wide transient arrays retained after collection: live=%d", live)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCArrayInitDataWideAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCArrayInitDataWideCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[4]++; v.InternalEntry[4]++ },
		"function":   func(v *Compiled) { v.Funcs[8].Params = []ValType{ValI32} },
		"type":       func(v *Compiled) { v.Types[1].Array.Mutable = false },
		"type-key":   func(v *Compiled) { v.FuncTypeID[7] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[1].ElemSize++ },
		"data": func(v *Compiled) {
			v.PassiveData[6].Bytes = slices.Clone(v.PassiveData[6].Bytes)
			v.PassiveData[6].Bytes[2] ^= 1
		},
		"export":  func(v *Compiled) { delete(v.Exports, "g8"); v.Exports["init"] = 10 },
		"feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCArrayInitData(candidate) {
				t.Fatal("hostile mutation retained wide array.init_data witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCArrayInitDataWideCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCArrayInitDataWideBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free wide array.init_data path used source compilation", err)
	}
}

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

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

const rv64PublicGCArrayPackedDataHex = "0061736d0100000001b3808080000a5e78005e7801600001640060027f6400017f60017f017f60037f64017f017f60027f7f017f6001646a017f6000017f600000038c808080000b020203040304050607080907c28080800007036e657700000c6e65772d6f766572666c6f770001056765745f750003056765745f730005077365745f6765740007036c656e00090964726f705f73656773000a0c8180808000010aa9818080000b8a808080000041014103fb0900000b928080800000418080808078418080808078fb0900000b89808080000020012000fb0d000b8880808000002000100010020b89808080000020012000fb0c000b8880808000002000100010040b928080800000200120002002fb0e0120012000fb0d010b908080800000200041014103fb090100200110060b8680808000002000fb0f0b868080800000100010080b858080800000fc09000b0b8880808000010105000102ff04"

func rv64PublicGCArrayPackedDataBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayPackedDataHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayPackedDataCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-array-packed-data.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCArrayPackedDataBytes(t))
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
		if err = os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, blob, 0600); err != nil {
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

func TestRV64PublicGCArrayPackedDataExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayPackedDataBytes(t)
	wantSource := [sha256.Size]byte{0x7f, 0xc4, 0xaf, 0xb6, 0xa2, 0xe3, 0xb2, 0xf6, 0xb1, 0x56, 0x2b, 0x4d, 0x01, 0x85, 0xb6, 0xd5, 0xd4, 0x42, 0x6c, 0x57, 0x9b, 0xcd, 0xa4, 0x4c, 0xce, 0x3b, 0x3a, 0x14, 0x01, 0x24, 0x7b, 0xce}
	if len(data) != 351 || sha256.Sum256(data) != wantSource {
		t.Fatal("packed data source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayPackedDataCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64ArtifactContractValid(view) || err != nil {
				t.Fatalf("missing packed data witness: %v", err)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default"}, {name: "stress", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}}} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime()
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
					if len(in.passiveDataDesc) != 16 || binary.LittleEndian.Uint32(in.passiveDataDesc[8:]) != 5 {
						t.Fatalf("passive descriptor=%x", in.passiveDataDesc)
					}
					raw, err := in.Invoke("new")
					if err != nil || len(raw) != 1 || raw[0] == 0 {
						t.Fatalf("new=%v err=%v", raw, err)
					}
					exact, owner, _, ok := in.refStore.gcRefExactType(raw[0])
					if !ok || owner != in || exact.Kind != ValueTypeReference || !exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.TypeIndex != 0 {
						t.Fatalf("packed token type=%#v owner=%p ok=%v", exact, owner, ok)
					}
					if err = in.ReleaseGCRef(ValueOf(ValAnyRef, raw[0]).GCRef()); err != nil {
						t.Fatal(err)
					}
					for _, tc := range []struct {
						name      string
						arg, want uint64
					}{{"get_u", 0xaaaa000000000002, 0xff}, {"get_s", 0xbbbb000000000002, uint64(^uint32(0))}} {
						if got, err := in.Invoke(tc.name, tc.arg); err != nil || !slices.Equal(got, []uint64{tc.want}) {
							t.Fatalf("%s=%v err=%v", tc.name, got, err)
						}
					}
					if got, err := in.Invoke("set_get", 0xcccc000000000001, 0xdddd000000000107); err != nil || !slices.Equal(got, []uint64{7}) {
						t.Fatalf("set_get=%v err=%v", got, err)
					}
					if got, err := in.Invoke("len"); err != nil || !slices.Equal(got, []uint64{3}) {
						t.Fatalf("len=%v err=%v", got, err)
					}
					before := in.gc.Stats()
					if _, err = in.Invoke("new-overflow"); err == nil {
						t.Fatal("overflowing array.new_data succeeded")
					}
					after := in.gc.Stats()
					if after.Allocations != before.Allocations {
						t.Fatalf("overflow mutated collector: before=%+v after=%+v", before, after)
					}
					if _, err = in.Invoke("get_u", 3); err == nil {
						t.Fatal("out-of-bounds packed get succeeded")
					}
					if _, err = in.Invoke("get_s", 3); err == nil {
						t.Fatal("out-of-bounds signed packed get succeeded")
					}
					if _, err = in.Invoke("set_get", 3, 7); err == nil {
						t.Fatal("out-of-bounds packed set succeeded")
					}
					if got, err := in.Invoke("drop_segs"); err != nil || len(got) != 0 {
						t.Fatalf("drop=%v err=%v", got, err)
					}
					if binary.LittleEndian.Uint32(in.passiveDataDesc[8:]) != 0 {
						t.Fatalf("dropped descriptor=%x", in.passiveDataDesc)
					}
					if got, err := in.Invoke("drop_segs"); err != nil || len(got) != 0 {
						t.Fatalf("repeated drop=%v err=%v", got, err)
					}
					if _, err = in.Invoke("new"); err == nil {
						t.Fatal("array.new_data after drop succeeded")
					}
				})
			}
		})
	}
}

func TestRV64PublicGCArrayPackedDataAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCArrayPackedDataCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[7]++; v.InternalEntry[7]++ },
		"function":   func(v *Compiled) { v.Funcs[6].Params[2] = ValI64 },
		"type":       func(v *Compiled) { v.Types[1].Array.Mutable = false },
		"type-key":   func(v *Compiled) { v.FuncTypeID[8] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[0].ElemSize++ },
		"data": func(v *Compiled) {
			v.PassiveData[0].Bytes = slices.Clone(v.PassiveData[0].Bytes)
			v.PassiveData[0].Bytes[2] ^= 1
		},
		"export":  func(v *Compiled) { delete(v.Exports, "new"); v.Exports["make"] = 0 },
		"feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if rv64ArtifactContractValid(candidate) {
				t.Fatal("hostile mutation retained packed witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutation admitted", err)
			}
			if !candidate.requiredFeatures.IsEnabled(CoreFeatureGC) {
				rv64RequireRestoredGCFeature(t, candidate)
			} else {
				rv64RequireRejectedArtifact(t, candidate)
			}
		})
	}
}

func TestRV64PublicGCArrayPackedDataCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCArrayPackedDataBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free packed data path used source compilation", err)
	}
}

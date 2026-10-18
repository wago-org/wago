//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const rv64PublicGCTypeSubtypingNonFlatExportHex = "0061736d0100000001a8808080000450006000017d5001006000017d4f01016000017d4e036000017d50006000017d5001046000017d03878080800006000102030405079f80808000060266310000026632000102663300020266340003026635000402663600050ac9808080000687808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b"

var rv64PublicGCTypeSubtypingNonFlatExportDigest = [sha256.Size]byte{0x0c, 0x14, 0x10, 0x8c, 0xfe, 0x32, 0x3e, 0x2d, 0xd3, 0x9f, 0xd8, 0xbf, 0xe3, 0x33, 0xbd, 0x92, 0x5e, 0x2f, 0x19, 0x32, 0x4e, 0x90, 0xcd, 0x1c, 0x3e, 0xe1, 0x61, 0x5f, 0xda, 0xd2, 0xbb, 0x60}

func rv64PublicGCTypeSubtypingNonFlatExportBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingNonFlatExportHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 183 || sha256.Sum256(data) != rv64PublicGCTypeSubtypingNonFlatExportDigest {
		t.Fatal("official non-flat export source identity changed")
	}
	return data
}

func rv64PublicGCTypeSubtypingNonFlatExportCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	source := rv64PublicGCTypeSubtypingNonFlatExportBytes(t)
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-non-flat-export.wago")
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
		return loaded
	}
	compiled, err := rv64I31Config().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !artifact && mode != "prepare" {
		return compiled
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		compiled.Close()
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		if err = os.WriteFile(path, blob, 0o600); err != nil {
			compiled.Close()
			t.Fatal(err)
		}
	}
	if !artifact {
		return compiled
	}
	loaded, err := LoadTrustedArtifact(blob)
	compiled.Close()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestRV64PublicGCTypeSubtypingNonFlatExportExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingNonFlatExportCompile(t, artifact)
			view := compiled.executionView()
			if !rv64ArtifactContractValid(view) || view.validate() != nil {
				t.Fatal("invalid public non-flat export execution contract")
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) || artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("non-flat-export unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}
			rt := NewRuntime()
			module, err := rt.Module(compiled)
			if err != nil {
				compiled.Close()
				rt.Close()
				t.Fatal(err)
			}
			instance, err := rt.Instantiate(context.Background(), module, WithImports(NewImports()))
			module.Close()
			if err != nil {
				compiled.Close()
				rt.Close()
				t.Fatal(err)
			}
			if instance.gc != nil || cache.base == 0 || len(cache.mem) < len(view.code) || !cache.sealed || (cache.flags&compiledCacheWritableCode != 0) != artifact || cache.refs == 0 {
				instance.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("non-flat-export publication/collector state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", instance.gc, cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}
			for i := 1; i <= 6; i++ {
				results, err := instance.Invoke(fmt.Sprintf("f%d", i))
				if err != nil || len(results) != 1 || results[0] != 0 {
					instance.Close()
					compiled.Close()
					rt.Close()
					t.Fatalf("f%d = %v, %v", i, results, err)
				}
			}
			var invokeErr error
			if allocs := testing.AllocsPerRun(1000, func() { _, invokeErr = instance.Invoke("f6") }); invokeErr != nil || allocs != 0 {
				instance.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("non-flat f6 steady state = %v, allocs=%v", invokeErr, allocs)
			}
			if err = compiled.Close(); err != nil {
				instance.Close()
				rt.Close()
				t.Fatal(err)
			}
			if cache.base == 0 || !cache.sealed || cache.refs == 0 {
				instance.Close()
				rt.Close()
				t.Fatal("live instance lost its executable lease")
			}
			if err = instance.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatalf("non-flat-export executable mapping survived final close: base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
			}
		})
	}

}

func TestRV64PublicGCTypeSubtypingNonFlatExportAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingNonFlatExportCompile(t, false)
	base := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[5] += 16; v.InternalEntry[5] += 16 },
		"function":   func(v *Compiled) { v.Funcs[1].TypeIndex = 0 },
		"type":       func(v *Compiled) { v.Types[0].Final = true },
		"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[0].Final = true },
		"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"export":     func(v *Compiled) { v.Exports["f6"] = 0 },
		"function-abi": func(v *Compiled) {
			v.nativeReferenceFunctionABI = !v.nativeReferenceFunctionABI
		},
		"missing-execution-contract": func(v *Compiled) { v.setRV64ExecutionContract(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if err := candidate.validate(); err == nil {
				t.Fatal("hostile retained non-flat export mutation admitted")
			}
			if name == "feature" {
				rv64RequireRestoredGCFeature(t, candidate)
				return
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
	compiled.Close()

}

func TestRV64PublicGCTypeSubtypingNonFlatExportCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingNonFlatExportBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free non-flat-export path used source compilation", err)
	}
}

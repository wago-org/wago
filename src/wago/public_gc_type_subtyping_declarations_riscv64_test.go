//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var rv64PublicGCTypeSubtypingDeclarationsHex = []string{
	"0061736d0100000001a8808080000750005e7f005001005e7f0050005e6e0050005e63000050005e64010050005e7f015001055e7f01",
	"0061736d0100000001b3808080000650005f005001005f005001015f017f005001025f027f006300005001035f037f006400007e015001045f037f006401007e01",
	"0061736d0100000001af808080000650005f005001005f00500060016401016e5001026001640001646e5001036001630001640050010460016b016401",
	"0061736d010000000199808080000350005f016e005001005f016401005001015f026401007f00",
	"0061736d0100000001a0808080000250005f027f006400004e025001005f027f006402005001005f027f00640100",
	"0061736d0100000001bb80808000024e0250005f027f0064010050005f027e006400004e035001015f037e006400007f005001005f037f006401007f005001015f037e006403007f00",
}

func rv64PublicGCTypeSubtypingDeclarationsBytes(t testing.TB, variant uint8) []byte {
	t.Helper()
	if variant == 0 || int(variant) > len(rv64PublicGCTypeSubtypingDeclarationsHex) {
		t.Fatal("invalid declaration variant", variant)
	}
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingDeclarationsHex[variant-1])
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64PublicGCTypeSubtypingDeclarationsExecutionAndArtifact(t *testing.T) {
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_MODE")
	for variant := uint8(1); int(variant) <= len(rv64PublicGCTypeSubtypingDeclarationsHex); variant++ {
		t.Run(strconv.Itoa(int(variant)), func(t *testing.T) {
			path := filepath.Join(dir, "gc-type-subtyping-declarations-"+strconv.Itoa(int(variant))+".wago")
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
				original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDeclarationsBytes(t, variant))
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
				if len(view.code) != 0 || len(view.Entry) != 0 || len(view.InternalEntry) != 0 || view.validate() != nil {
					t.Fatal("invalid public declaration-only metadata")
				}
				rt := NewRuntime()
				mod, err := rt.Module(compiled)
				if err != nil {
					rt.Close()
					t.Fatal(err)
				}
				in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
				mod.Close()
				if err != nil {
					rt.Close()
					t.Fatal(err)
				}
				if in.gc != nil || compiled.codeCache.base != 0 || compiled.codeCache.mem != nil || compiled.codeCache.sealed || compiled.codeCache.flags&compiledCacheWritableCode != 0 {
					t.Fatalf("declaration-only instance allocated collector/code: gc=%p base=%#x mem=%d", in.gc, compiled.codeCache.base, len(compiled.codeCache.mem))
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				rt.Close()
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingDeclarationsAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	for variant := uint8(1); int(variant) <= len(rv64PublicGCTypeSubtypingDeclarationsHex); variant++ {
		compiled, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDeclarationsBytes(t, variant))
		if err != nil {
			t.Fatal(err)
		}
		base := compiled.executionView()
		for name, mutate := range map[string]func(*Compiled){
			"type":       func(v *Compiled) { v.Types[0].Final = !v.Types[0].Final },
			"descriptor": func(v *Compiled) { v.GCTypeDescs[0].Final = !v.GCTypeDescs[0].Final },
			"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		} {
			t.Run(strconv.Itoa(int(variant))+"/"+name, func(t *testing.T) {
				candidate := rv64ReferenceStartCopy(base)
				mutate(candidate)
				if err := candidate.validate(); err == nil {
					t.Fatal("hostile retained declaration mutation admitted")
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
}

func TestRV64PublicGCTypeSubtypingDeclarationsCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := uint8(1); int(variant) <= len(rv64PublicGCTypeSubtypingDeclarationsHex); variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDeclarationsBytes(t, variant))
		if c != nil {
			_ = c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free declaration-only path used source compilation", variant, err)
		}
	}
}

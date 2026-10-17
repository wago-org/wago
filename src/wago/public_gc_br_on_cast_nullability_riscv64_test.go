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

const rv64PublicGCBrOnCastNullabilityHex = "0061736d01000000019680808000045f006001646e01640060016e01640060016e016300038480808000030102030abb80808000038f808080000002646e2000fb1800016e000b000b8e8080800000026e2000fb1801016e000b000b8e8080800000026e2000fb1803016e000b000b"

const rv64PublicGCBrOnCastFailNullabilityHex = "0061736d01000000018f80808000035f006001646e01646e60016e016e038480808000030102020aba80808000038e80808000000264002000fb1900016e000b0b8e80808000000264002000fb1901016e000b0b8e80808000000263002000fb1903016e000b0b"

func rv64PublicGCBrOnCastNullabilityBytes(t testing.TB, fail bool) []byte {
	t.Helper()
	source := rv64PublicGCBrOnCastNullabilityHex
	if fail {
		source = rv64PublicGCBrOnCastFailNullabilityHex
	}
	b, err := hex.DecodeString(source)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The official nullability modules intentionally export no functions. After
// ordinary module admission and instantiation have completed, expose only the
// two nullable-parameter definitions through the package-private directory so
// QEMU executes their genuine machine code. Restore the immutable directory
// before any subsequent runtime validation or artifact operation.
func rv64InvokeGCBrOnCastNullabilityInternals(t testing.TB, in *Instance, fail bool) {
	t.Helper()
	old := in.c.Exports
	in.c.Exports = map[string]int{"nullable-nonnull": 1, "nullable-nullable": 2}
	defer func() { in.c.Exports = old }()
	if fail {
		for _, name := range []string{"nullable-nonnull", "nullable-nullable"} {
			if got, err := in.Invoke(name, 0); err != nil || !slices.Equal(got, []uint64{0}) {
				t.Fatalf("%s(null)=%v,%v want null", name, got, err)
			}
		}
		return
	}
	if got, err := in.Invoke("nullable-nonnull", 0); err == nil || len(got) != 0 {
		t.Fatalf("successful-cast nullable->nonnull(null)=%v,%v want unreachable trap", got, err)
	}
	if got, err := in.Invoke("nullable-nullable", 0); err != nil || !slices.Equal(got, []uint64{0}) {
		t.Fatalf("successful-cast nullable->nullable(null)=%v,%v want null", got, err)
	}
}

func TestRV64PublicGCBrOnCastNullabilityExecutionAndArtifact(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "fail"}[fail], func(t *testing.T) {
			data := rv64PublicGCBrOnCastNullabilityBytes(t, fail)
			wantDigest := [sha256.Size]byte{0xcc, 0x5b, 0xde, 0xb4, 0xb5, 0x74, 0x09, 0xc6, 0xe1, 0x94, 0xae, 0x09, 0x4f, 0x2a, 0x50, 0xec, 0xe4, 0xdc, 0xe6, 0xaf, 0xa6, 0x6c, 0xd2, 0x5e, 0xc0, 0x7b, 0x46, 0x24, 0xcc, 0xb9, 0x66, 0x32}
			if fail {
				wantDigest = [sha256.Size]byte{0x67, 0x25, 0xd0, 0xfa, 0x74, 0x19, 0x74, 0xc1, 0x21, 0x4c, 0xda, 0xbc, 0x35, 0xfa, 0x96, 0x27, 0x79, 0xf7, 0x4a, 0xbd, 0x71, 0x1c, 0xc2, 0x1b, 0x04, 0xd6, 0x7d, 0x0c, 0x40, 0x73, 0x57, 0x8e}
			}
			if sha256.Sum256(data) != wantDigest {
				t.Fatal("nullability branch-cast source identity changed")
			}
			dir, mode := os.Getenv("WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_MODE")
			name := "gc-br-on-cast-nullability.wago"
			if fail {
				name = "gc-br-on-cast-fail-nullability.wago"
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
				if !rv64ArtifactContractValid(view) || railshotValidateCompiled(view) != nil {
					t.Fatalf("invalid branch-cast execution contract: %v", railshotValidateCompiled(view))
				}
				if params, results, err := view.maxCallSlots(); err != nil || params != 1 || results != 1 {
					t.Fatalf("call capacity=%d/%d,%v want 1/1", params, results, err)
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
					if in.gc == nil || in.existingGCRefTestTableState() != nil {
						t.Fatalf("nullability collector/table state = %p/%p", in.gc, in.existingGCRefTestTableState())
					}
					rv64InvokeGCBrOnCastNullabilityInternals(t, in, fail)
					if err := in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 0 {
						t.Fatalf("live objects=%d want 0 for validation-only module", live)
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

func TestRV64PublicGCBrOnCastNullabilityAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "fail"}[fail], func(t *testing.T) {
			compiled, err := rv64I31Config().Compile(rv64PublicGCBrOnCastNullabilityBytes(t, fail))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			base := compiled.executionView()
			for name, mutate := range map[string]func(*Compiled){
				"code":        func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
				"entry":       func(v *Compiled) { v.Entry[2]++; v.InternalEntry[2]++ },
				"function":    func(v *Compiled) { v.Funcs[1].Results = []ValType{ValI32} },
				"type-result": func(v *Compiled) { v.Types[1].Results[0].Ref.Nullable = !v.Types[1].Results[0].Ref.Nullable },
				"type-key":    func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
				"descriptor":  func(v *Compiled) { v.GCTypeDescs[0].Align = 2 },
				"export":      func(v *Compiled) { v.Exports["other"] = 0 },
				"feature":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
			} {
				t.Run(name, func(t *testing.T) {
					candidate := rv64ReferenceStartCopy(base)
					mutate(candidate)
					if rv64ArtifactContractValid(candidate) {
						t.Fatal("hostile retained mutation kept nullability branch-cast witness")
					}
					if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
						t.Fatal("hostile retained nullability branch-cast mutation admitted", err)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCBrOnCastNullabilityCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for _, fail := range []bool{false, true} {
		c, err := rv64I31Config().Compile(rv64PublicGCBrOnCastNullabilityBytes(t, fail))
		if c != nil {
			_ = c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free nullability branch-cast path used source compilation", err)
		}
	}
}

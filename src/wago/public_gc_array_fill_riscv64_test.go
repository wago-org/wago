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
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64PublicGCArrayFillHex = "0061736d01000000019580808000055e78005e780160017f017f60000060037f7f7f000384808080000302030406958080800002640000410a410cfb06000b640101410cfb07010b07b080808000030d61727261795f6765745f6e746800000f61727261795f66696c6c2d6e756c6c00010a61727261795f66696c6c00020ab3808080000389808080000023012000fb0d010b8d8080800000d001410041004100fb10010b8d80808000002301200020012002fb10010b"

func rv64PublicGCArrayFillBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayFillHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayFillCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-array-fill.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCArrayFillBytes(t))
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

func rv64AssertGCArrayFillTrap(t *testing.T, in *Instance, name string, code coreruntime.TrapCode, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(name, args...)
	if got != nil || err == nil {
		t.Fatalf("trapping %s%v=%v,%v", name, args, got, err)
	}
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != code {
		t.Fatalf("trapping %s%v returned %v, want trap %v", name, args, err, code)
	}
}

func TestRV64PublicGCArrayFillExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayFillBytes(t)
	wantSource := [sha256.Size]byte{0x08, 0x93, 0xca, 0xa7, 0xae, 0x7a, 0xb2, 0xd8, 0x70, 0x32, 0x9d, 0xa9, 0x69, 0x7d, 0x40, 0x5a, 0x51, 0x59, 0x2c, 0xb3, 0xec, 0xc1, 0xb4, 0xb8, 0x33, 0x78, 0x0e, 0xf9, 0xb2, 0x58, 0x01, 0x69}
	if len(data) != 183 || sha256.Sum256(data) != wantSource {
		t.Fatal("array.fill source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayFillCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64GCArrayFill(view) || !rv64GCArrayFillExecutionCode(view) || err != nil {
				t.Fatalf("missing exact array.fill witness: validation=%v product=%v code=%d/%x entries=%v/%v globals=%#v", err, view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.memoryDir.gcArrayGlobals)
			}
			if params, results, err := railshotImportedCallSlots(view, 3, 1); err != nil || params != 5 || results != 1 {
				t.Fatalf("array.fill call arena=%d/%d,%v, want 5/1", params, results, err)
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
					if len(in.serArgs) < 5*8 {
						t.Fatalf("array.fill argument arena=%d bytes, want at least 40", len(in.serArgs))
					}
					rv64AssertGCArrayFillTrap(t, in, "array_fill-null", coreruntime.TrapNullReference)
					rv64AssertGCArrayFillTrap(t, in, "array_fill", coreruntime.TrapBuiltin, 13, 0, 0)
					rv64AssertGCArrayFillTrap(t, in, "array_fill", coreruntime.TrapBuiltin, 0, 0, 13)
					if got, err := in.Invoke("array_fill", 0xffff00000000000c, 0, 0xffff000000000000); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper zero-length fill=%v,%v", got, err)
					}
					for _, index := range []uint64{0, 5, 11} {
						if got, err := in.Invoke("array_get_nth", index); err != nil || !slices.Equal(got, []uint64{0}) {
							t.Fatalf("array_get_nth(%d)=%v,%v want 0", index, got, err)
						}
					}
					rv64AssertGCArrayFillTrap(t, in, "array_get_nth", coreruntime.TrapBuiltin, 12)
					if got, err := in.Invoke("array_fill", 0xaaaa000000000002, 0xbbbb00000000010b, 0xcccc000000000002); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper array.fill=%v,%v", got, err)
					}
					for index, want := range map[uint64]uint64{1: 0, 2: 11, 3: 11, 4: 0} {
						if got, err := in.Invoke("array_get_nth", index); err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatalf("array_get_nth(%d)=%v,%v want %d", index, got, err, want)
						}
					}
					if err = in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 2 {
						t.Fatalf("live objects=%d, want two rooted arrays", live)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCArrayFillAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCArrayFillCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":        func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":       func(v *Compiled) { v.Entry[2]++; v.InternalEntry[2]++ },
		"function":    func(v *Compiled) { v.Funcs[2].Params[2] = ValI64 },
		"type":        func(v *Compiled) { v.Types[1].Array.Mutable = false },
		"type-key":    func(v *Compiled) { v.FuncTypeID[2] ^= 1 },
		"descriptor":  func(v *Compiled) { v.GCTypeDescs[1].ElemSize++ },
		"global":      func(v *Compiled) { v.Globals[1].Mutable = false },
		"initializer": func(v *Compiled) { v.memoryDir.gcArrayGlobals[0].Bits[0]++ },
		"export":      func(v *Compiled) { delete(v.Exports, "array_fill"); v.Exports["fill"] = 2 },
		"feature":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			if candidate.memoryDir != nil {
				dir := *candidate.memoryDir
				dir.gcArrayGlobals = slices.Clone(dir.gcArrayGlobals)
				for i := range dir.gcArrayGlobals {
					dir.gcArrayGlobals[i].Bits = slices.Clone(dir.gcArrayGlobals[i].Bits)
				}
				candidate.memoryDir = &dir
			}
			mutate(candidate)
			if rv64GCArrayFill(candidate) {
				t.Fatal("hostile mutation retained array.fill witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCArrayFillCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCArrayFillBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free array.fill path used source compilation", err)
	}
}

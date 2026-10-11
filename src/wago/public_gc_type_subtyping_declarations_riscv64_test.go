//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

const rv64PublicGCTypeSubtypingDeclarationsHex = "0061736d0100000001a8808080000750005e7f005001005e7f0050005e6e0050005e63000050005e64010050005e7f015001055e7f01"

func rv64PublicGCTypeSubtypingDeclarationsBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingDeclarationsHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64PublicGCTypeSubtypingDeclarationsExecutionAndArtifact(t *testing.T) {
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-declarations.wago")
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
		original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDeclarationsBytes(t))
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
	for i, compiled := range compiledSet {
		defer compiled.Close()
		view := compiled.executionView()
		if !rv64GCTypeSubtypingDeclarations(view) || view.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingDeclarations || len(view.code) != 0 || len(view.Entry) != 0 || len(view.InternalEntry) != 0 || railshotValidateCompiled(view) != nil {
			t.Fatalf("missing exact declaration-only type-subtyping witness at %d: exact=%t product=%v struct=%v array=%v staged=%v native_gc_abi=%d code=%d entries=%v/%v types=%#v descs=%#v validation=%v", i, rv64GCTypeSubtypingDeclarations(view), view.stagedGCTypeSubtypingProduct(), view.stagedGCStructProduct(), view.stagedGCArrayProduct(), view.stagedFeatures(), view.nativeGCABIRequirement(), len(view.code), view.Entry, view.InternalEntry, view.Types, view.GCTypeDescs, railshotValidateCompiled(view))
		}
		rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
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
}

func TestRV64PublicGCTypeSubtypingDeclarationsAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDeclarationsBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	base := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"type":       func(v *Compiled) { v.Types[1].Supers = nil },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[1].HasSuper = false },
		"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingDeclarations(candidate) {
				t.Fatal("hostile retained mutation kept declaration-only witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained declaration-only mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingDeclarationsCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDeclarationsBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free declaration-only path used source compilation", err)
	}
}

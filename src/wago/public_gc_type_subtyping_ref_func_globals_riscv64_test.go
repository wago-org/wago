//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func rv64GCTypeSubtypingRefFuncGlobalPins() []stagedGCTypeSubtypingProductPin {
	var pins []stagedGCTypeSubtypingProductPin
	for _, pin := range stagedGCTypeSubtypingProductPins {
		if pin.Class == stagedGCTypeSubtypingRefFuncGlobals {
			pins = append(pins, pin)
		}
	}
	return pins
}

func rv64GCTypeSubtypingRefFuncGlobalBytes(t testing.TB, variant uint8) []byte {
	t.Helper()
	pins := rv64GCTypeSubtypingRefFuncGlobalPins()
	if variant == 0 || int(variant) > len(pins) {
		t.Fatal("invalid ref.func-global variant", variant)
	}
	pin := pins[variant-1]
	wire, err := hex.DecodeString(pin.Hex)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := hex.DecodeString(pin.SHA256)
	gotDigest := sha256.Sum256(wire)
	if err != nil || len(wire) != pin.Size || len(wantDigest) != len(gotDigest) {
		t.Fatal("ref.func-global source identity changed", variant, err)
	}
	for i := range gotDigest {
		if gotDigest[i] != wantDigest[i] {
			t.Fatal("ref.func-global source digest changed", variant)
		}
	}
	return wire
}

func rv64CompileGCTypeSubtypingRefFuncGlobal(t testing.TB, variant uint8, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-ref-func-globals-"+strconv.Itoa(int(variant))+".wago")
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
	original, err := rv64I31Config().Compile(rv64GCTypeSubtypingRefFuncGlobalBytes(t, variant))
	if err != nil {
		t.Fatal(variant, err)
	}
	if !artifact && mode != "prepare" {
		return original
	}
	blob, err := original.MarshalBinary()
	if err != nil {
		original.Close()
		t.Fatal(variant, err)
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
	if !artifact {
		return original
	}
	original.Close()
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(variant, err)
	}
	return loaded
}

func TestRV64GCTypeSubtypingRefFuncGlobalsSourceAndArtifact(t *testing.T) {
	if len(rv64GCTypeSubtypingRefFuncGlobalPins()) != 6 {
		t.Fatal("official ref.func-global inventory changed")
	}
	for variant := uint8(1); variant <= 6; variant++ {
		for _, artifact := range []bool{false, true} {
			t.Run(strconv.Itoa(int(variant))+"/artifact="+strconv.FormatBool(artifact), func(t *testing.T) {
				compiled := rv64CompileGCTypeSubtypingRefFuncGlobal(t, variant, artifact)
				view := compiled.executionView()
				validation := railshotValidateCompiled(view)
				if got := rv64GCTypeSubtypingRefFuncGlobalsProfileWithCode(view, false, true); got != variant || !rv64GCTypeSubtypingRefFuncGlobals(view) ||
					view.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingRefFuncGlobals || validation != nil {
					compiled.Close()
					t.Fatalf("missing exact ref.func-global witness: got=%d want=%d product=%v validation=%v", got, variant, view.stagedGCTypeSubtypingProduct(), validation)
				}
				cache := compiled.loadCodeCache()
				if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
					artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
					compiled.Close()
					t.Fatalf("unexpected unpublished mapping state artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
				}
				cfg := rv64I31Config()
				cfg.railshotState.tailProducts = true
				rt := NewRuntime(WithRuntimeConfig(cfg))
				mod, err := rt.Module(compiled)
				if err != nil {
					compiled.Close()
					rt.Close()
					t.Fatal(err)
				}
				in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
				mod.Close()
				if err != nil {
					compiled.Close()
					rt.Close()
					t.Fatal(err)
				}
				if in.gc != nil || len(in.funcRefDescs) != (len(view.Funcs)+1)*coreruntime.FuncRefDescBytes || len(in.globalCells) != len(view.Globals) {
					in.Close()
					compiled.Close()
					rt.Close()
					t.Fatalf("ref.func-global instance state collector=%p descs=%d globals=%d", in.gc, len(in.funcRefDescs), len(in.globalCells))
				}
				for globalIndex, global := range view.Globals {
					value := readGlobalObject(in.globalCells[globalIndex], ValFuncRef)
					want := uint64(offHeapSlicePtr(in.funcRefDescs)) + uint64(global.InitFunc+1)*coreruntime.FuncRefDescBytes
					if value != want {
						in.Close()
						compiled.Close()
						rt.Close()
						t.Fatalf("global %d descriptor=%#x want=%#x for function %d", globalIndex, value, want, global.InitFunc)
					}
				}
				if cache.base == 0 || !cache.sealed || cache.refs == 0 {
					in.Close()
					compiled.Close()
					rt.Close()
					t.Fatal("instantiation did not publish and lease executable code")
				}
				if err = compiled.Close(); err != nil {
					in.Close()
					rt.Close()
					t.Fatal(err)
				}
				if cache.base == 0 || cache.refs == 0 {
					in.Close()
					rt.Close()
					t.Fatal("live instance lost its executable lease")
				}
				if err = in.Close(); err != nil {
					rt.Close()
					t.Fatal(err)
				}
				rt.Close()
				if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
					t.Fatalf("executable mapping survived final close base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
				}
			})
		}
	}
}

func TestRV64GCTypeSubtypingRefFuncGlobalsRejectHostileRetainedState(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	for variant := uint8(1); variant <= 6; variant++ {
		compiled := rv64CompileGCTypeSubtypingRefFuncGlobal(t, variant, false)
		base := compiled.executionView()
		for name, mutate := range map[string]func(*Compiled){
			"code":       func(v *Compiled) { v.code[len(v.code)-1] ^= 1 },
			"entry":      func(v *Compiled) { v.Entry[0] += 4; v.InternalEntry[0] += 4 },
			"function":   func(v *Compiled) { v.Funcs[0].TypeIndex ^= 1 },
			"type":       func(v *Compiled) { v.Types[0].Final = !v.Types[0].Final },
			"type-key":   func(v *Compiled) { v.FuncTypeID[0] ^= 1 },
			"descriptor": func(v *Compiled) { v.GCTypeDescs[0].Final = !v.GCTypeDescs[0].Final },
			"global":     func(v *Compiled) { v.Globals[0].InitFunc = uint32(len(v.Funcs)) },
			"value-type": func(v *Compiled) { v.ValueTypes[0].Ref.Nullable = true },
			"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
			"product":    func(v *Compiled) { v.codeCache.gcTypeSubtypingProduct = 0 },
		} {
			t.Run(strconv.Itoa(int(variant))+"/"+name, func(t *testing.T) {
				candidate := cloneCompiledMetadata(base)
				candidate.validateMemo = &validateMemo{}
				mutate(candidate)
				if rv64GCTypeSubtypingRefFuncGlobals(candidate) {
					t.Fatal("hostile retained mutation kept exact witness")
				}
				if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("hostile retained mutation admitted", err)
				}
			})
		}
		compiled.Close()
	}
}

func TestRV64GCTypeSubtypingRefFuncGlobalsCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := uint8(1); variant <= 6; variant++ {
		compiled, err := rv64I31Config().Compile(rv64GCTypeSubtypingRefFuncGlobalBytes(t, variant))
		if compiled != nil {
			compiled.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free ref.func-global path used source compilation", variant, err)
		}
	}
}

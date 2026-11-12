//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"

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

	original, err := rv64I31Config().Compile(rv64GCTypeSubtypingRefFuncGlobalBytes(t, variant))
	if err != nil {
		t.Fatal(variant, err)
	}

	return original

}

func TestRV64GCTypeSubtypingRefFuncGlobalsSourceAndArtifact(t *testing.T) {
	if len(rv64GCTypeSubtypingRefFuncGlobalPins()) != 6 {
		t.Fatal("official ref.func-global inventory changed")
	}
	for variant := uint8(1); variant <= 6; variant++ {
		for _, artifact := range []bool{false} {
			t.Run(strconv.Itoa(int(variant))+"/artifact="+strconv.FormatBool(artifact), func(t *testing.T) {
				compiled := rv64CompileGCTypeSubtypingRefFuncGlobal(t, variant, artifact)
				view := compiled.executionView()
				if !rv64SourceContractValid(view) || view.validate() != nil {
					t.Fatal("invalid public ref.func global execution contract")
				}
				cache := compiled.loadCodeCache()
				if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
					artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
					compiled.Close()
					t.Fatalf("unexpected unpublished mapping state artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
				}
				rt := NewRuntime()
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

func TestRV64GCTypeSubtypingRefFuncGlobalsCodeFingerprintCollisionCompatibility(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run("artifact="+strconv.FormatBool(artifact), func(t *testing.T) {
			compiled := rv64NativeTailCompile(t, "ref-func-global-code-collision", rv64PublicNativeIdentityModule(nil), artifact)
			defer compiled.Close()
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || view.stagedGCTypeSubtypingProduct() != 0 {
				t.Fatal("ordinary numeric identity lost its public execution contract")
			}
			if err := railshotValidateCompiled(view); err != nil {
				t.Fatal("ordinary numeric identity rejected by a GC-only code fingerprint", err)
			}
		})
	}
}

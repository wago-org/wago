//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64PublicGCTypeSubtypingRefTestDirectionFalseHex = "0061736d0100000001b180808000054e0250006000005f016400004e0250006000005f016400004e025001006000005f004e025001026000005f006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"

func rv64PublicGCTypeSubtypingRefTestDirectionFalseBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRefTestDirectionFalseHex)
	want := [sha256.Size]byte{0x9b, 0x81, 0x11, 0xee, 0x2e, 0x3f, 0xb9, 0x1c, 0xc7, 0x80, 0x1a, 0x63, 0xb0, 0xa5, 0xa8, 0xe9, 0x7e, 0xca, 0x7b, 0x56, 0x65, 0xf7, 0xe6, 0xfe, 0xd5, 0xbe, 0x8a, 0x83, 0x27, 0x53, 0x42, 0x13}
	if err != nil || len(b) != 122 || sha256.Sum256(b) != want {
		t.Fatal("direction-false ref.test source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRefTestDirectionFalseCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestDirectionFalseBytes(t))
	if err != nil {
		t.Fatal(err)
	}

	return original

}

func TestRV64PublicGCTypeSubtypingRefTestDirectionFalseExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRefTestDirectionFalseCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || view.validate() != nil {
				compiled.Close()
				t.Fatal("missing public ref.test execution contract")
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("direction-false ref.test unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
			if in.gc != nil || len(in.funcRefDescs) != (len(view.Funcs)+1)*coreruntime.FuncRefDescBytes || cache.base == 0 || !cache.sealed || cache.refs == 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatal("direction-false ref.test runtime state is incomplete")
			}
			got, err := in.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("run = %v, %v; want [0]", got, err)
			}
			if err = compiled.Close(); err != nil || cache.base == 0 || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live direction-false ref.test instance lost its code lease", err)
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatal("direction-false ref.test executable mapping survived final close")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRefTestDirectionFalsePublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestDirectionFalseCompile(t, false)
	rt := NewRuntime()
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	compiled.Close()
	rt.Close()
	if err != nil {
		t.Fatal("direction-false ref.test public runtime instantiation failed", err)
	}
}

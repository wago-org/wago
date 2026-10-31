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

const rv64PublicGCTypeSubtypingRefTestNominalFalseHex = "0061736d01000000019f80808000034e0250006000005001006000004e0250006000005001006000006000017f038380808000020204078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14000b"

func rv64PublicGCTypeSubtypingRefTestNominalFalseBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRefTestNominalFalseHex)
	want := [sha256.Size]byte{0x28, 0x41, 0xd0, 0x98, 0xdf, 0xca, 0x12, 0x5c, 0xcd, 0x9c, 0x57, 0x7c, 0xf5, 0x57, 0x62, 0x74, 0x4c, 0x8a, 0x39, 0x11, 0xa1, 0x98, 0x6f, 0x85, 0x7b, 0xe4, 0x8e, 0xbc, 0x0d, 0x51, 0xf7, 0x35}
	if err != nil || len(b) != 104 || sha256.Sum256(b) != want {
		t.Fatal("nominal-false ref.test source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRefTestNominalFalseCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestNominalFalseBytes(t))
	if err != nil {
		t.Fatal(err)
	}

	return original

}

func TestRV64PublicGCTypeSubtypingRefTestNominalFalseExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRefTestNominalFalseCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || view.validate() != nil {
				compiled.Close()
				t.Fatal("missing public ref.test execution contract")
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) || artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatal("nominal-false ref.test unpublished mapping state")
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
				t.Fatal("nominal-false ref.test runtime state is incomplete")
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
				t.Fatal("live nominal-false ref.test instance lost its code lease", err)
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatal("nominal-false ref.test executable mapping survived final close")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRefTestNominalFalsePublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestNominalFalseCompile(t, false)
	rt := NewRuntime()
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	compiled.Close()
	rt.Close()
	if err != nil {
		t.Fatal("nominal-false ref.test public runtime instantiation failed", err)
	}
}

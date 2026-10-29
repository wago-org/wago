//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

const rv64PublicGCTypeSubtypingRuntimeTypedTableCallHex = "0061736d01000000019580808000045000600000500100600000500101600000600000038680808000050102030303048680808000016301010202079780808000030372756e0002056661696c310003056661696c320004098f8080800001060041000b630102d2000bd2010b0ac780808000058280808000000b8280808000000b9b8080800000410011000041011100004100110100410111010041011102000b87808080000041001102000b87808080000041001103000b"

func rv64PublicGCTypeSubtypingRuntimeTypedTableCallBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRuntimeTypedTableCallHex)
	want := [sha256.Size]byte{0x2a, 0xd9, 0x54, 0x57, 0x82, 0x1c, 0xeb, 0x82, 0x11, 0xd5, 0x73, 0x3f, 0xe3, 0x08, 0xf0, 0x31, 0xf1, 0x10, 0x37, 0x55, 0x73, 0x3b, 0xbf, 0x8b, 0x5d, 0xb9, 0xc8, 0x5d, 0xb0, 0xeb, 0x6d, 0x9b}
	if err != nil || len(b) != 186 || sha256.Sum256(b) != want {
		t.Fatal("runtime typed-table call source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRuntimeTypedTableCallCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRuntimeTypedTableCallBytes(t))
	if err != nil {
		t.Fatal(err)
	}

	return original

}

func TestRV64PublicGCTypeSubtypingRuntimeTypedTableCallExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRuntimeTypedTableCallCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || view.validate() != nil {
				compiled.Close()
				t.Fatal("missing public subtype execution contract")
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("runtime typed-table unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
			base, memoryBytes, codeBytes, sealed, writable, refs := cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs
			if in.gc != nil || base == 0 || memoryBytes < codeBytes || !sealed || writable != artifact || refs == 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("runtime typed-table publication state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", in.gc, base, memoryBytes, codeBytes, sealed, writable, refs)
			}
			if got, err := in.Invoke("run"); err != nil || len(got) != 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("run = %v, %v; want empty success", got, err)
			}
			for i := 1; i <= 2; i++ {
				rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail"+string(rune('0'+i)), TrapIndirectWrongSig)
			}
			if got, err := in.Invoke("run"); err != nil || len(got) != 0 {
				t.Fatalf("run after traps = %v, %v; want empty success", got, err)
			}
			if err = compiled.Close(); err != nil {
				in.Close()
				rt.Close()
				t.Fatal(err)
			}
			if cache.base == 0 || !cache.sealed || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live runtime typed-table instance lost its executable lease")
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatalf("runtime typed-table executable mapping survived final close: base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeTypedTableCallPublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRuntimeTypedTableCallCompile(t, false)
	defer compiled.Close()
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
	if in != nil {
		_ = in.Close()
	}
	if err != nil {
		t.Fatal("runtime typed-table call instantiated without private Runtime permission", err)
	}
}

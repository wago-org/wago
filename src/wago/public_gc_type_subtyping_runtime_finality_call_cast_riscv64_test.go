//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

const rv64PublicGCTypeSubtypingRuntimeFinalityCallCastHex = "0061736d0100000001898080800002500060000060000003878080800006000101010101048580808000017001020207a18080800004056661696c310002056661696c320003056661696c330004056661696c340005098c80808000010441000b02d2000bd2010b0acb80808000068280808000000b8280808000000b8a8080800000024041011100000b0b8a8080800000024041001101000b0b8a808080000041012500fb16001a0b8a808080000041002500fb16011a0b"

func rv64PublicGCTypeSubtypingRuntimeFinalityCallCastBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRuntimeFinalityCallCastHex)
	want := [sha256.Size]byte{0x37, 0x5a, 0x32, 0x7f, 0x84, 0x69, 0xd4, 0x1d, 0x4f, 0x15, 0xf0, 0x51, 0x09, 0x53, 0x3a, 0x90, 0x12, 0x7f, 0xc5, 0x28, 0x74, 0x14, 0x36, 0x4e, 0x22, 0x72, 0x03, 0xd7, 0xd4, 0x8e, 0x76, 0x62}
	if err != nil || len(b) != 185 || sha256.Sum256(b) != want {
		t.Fatal("runtime finality call/cast source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRuntimeFinalityCallCastCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRuntimeFinalityCallCastBytes(t))
	if err != nil {
		t.Fatal(err)
	}

	return original

}

func TestRV64PublicGCTypeSubtypingRuntimeFinalityCallCastExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRuntimeFinalityCallCastCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || view.validate() != nil {
				compiled.Close()
				t.Fatal("missing public subtype execution contract")
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("runtime finality unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
				t.Fatalf("runtime finality publication state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", in.gc, base, memoryBytes, codeBytes, sealed, writable, refs)
			}
			for i := 1; i <= 2; i++ {
				rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail"+string(rune('0'+i)), TrapIndirectWrongSig)
			}
			for i := 3; i <= 4; i++ {
				rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail"+string(rune('0'+i)), TrapCastFailure)
			}
			// Re-enter after both helper-mediated trap classes to prove that the
			// framed stack and invocation context were restored.
			rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail1", TrapIndirectWrongSig)
			rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail3", TrapCastFailure)
			if err = compiled.Close(); err != nil {
				in.Close()
				rt.Close()
				t.Fatal(err)
			}
			if cache.base == 0 || !cache.sealed || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live runtime finality instance lost its executable lease")
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatalf("runtime finality executable mapping survived final close: base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeFinalityCallCastPublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRuntimeFinalityCallCastCompile(t, false)
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
		t.Fatal("runtime finality call/cast instantiated without private Runtime permission", err)
	}
}

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

const rv64PublicGCTypeSubtypingNonFlatExportHex = "0061736d0100000001a8808080000450006000017d5001006000017d4f01016000017d4e036000017d50006000017d5001046000017d03878080800006000102030405079f80808000060266310000026632000102663300020266340003026635000402663600050ac9808080000687808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b"

var rv64PublicGCTypeSubtypingNonFlatExportDigest = [sha256.Size]byte{0x0c, 0x14, 0x10, 0x8c, 0xfe, 0x32, 0x3e, 0x2d, 0xd3, 0x9f, 0xd8, 0xbf, 0xe3, 0x33, 0xbd, 0x92, 0x5e, 0x2f, 0x19, 0x32, 0x4e, 0x90, 0xcd, 0x1c, 0x3e, 0xe1, 0x61, 0x5f, 0xda, 0xd2, 0xbb, 0x60}

func rv64PublicGCTypeSubtypingNonFlatExportBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCTypeSubtypingNonFlatExportHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 183 || sha256.Sum256(data) != rv64PublicGCTypeSubtypingNonFlatExportDigest {
		t.Fatal("official non-flat export source identity changed")
	}
	return data
}

func rv64PublicGCTypeSubtypingNonFlatExportCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	source := rv64PublicGCTypeSubtypingNonFlatExportBytes(t)

	compiled, err := rv64I31Config().Compile(source)
	if err != nil {
		t.Fatal(err)
	}

	return compiled

}

func TestRV64PublicGCTypeSubtypingNonFlatExportExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingNonFlatExportCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || view.validate() != nil {
				t.Fatal("invalid public non-flat export execution contract")
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) || artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("non-flat-export unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}
			rt := NewRuntime()
			module, err := rt.Module(compiled)
			if err != nil {
				compiled.Close()
				rt.Close()
				t.Fatal(err)
			}
			instance, err := rt.Instantiate(context.Background(), module, WithImports(NewImports()))
			module.Close()
			if err != nil {
				compiled.Close()
				rt.Close()
				t.Fatal(err)
			}
			if instance.gc != nil || cache.base == 0 || len(cache.mem) < len(view.code) || !cache.sealed || (cache.flags&compiledCacheWritableCode != 0) != artifact || cache.refs == 0 {
				instance.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("non-flat-export publication/collector state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", instance.gc, cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}
			for i := 1; i <= 6; i++ {
				results, err := instance.Invoke(fmt.Sprintf("f%d", i))
				if err != nil || len(results) != 1 || results[0] != 0 {
					instance.Close()
					compiled.Close()
					rt.Close()
					t.Fatalf("f%d = %v, %v", i, results, err)
				}
			}
			var invokeErr error
			if allocs := testing.AllocsPerRun(1000, func() { _, invokeErr = instance.Invoke("f6") }); invokeErr != nil || allocs != 0 {
				instance.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("non-flat f6 steady state = %v, allocs=%v", invokeErr, allocs)
			}
			if err = compiled.Close(); err != nil {
				instance.Close()
				rt.Close()
				t.Fatal(err)
			}
			if cache.base == 0 || !cache.sealed || cache.refs == 0 {
				instance.Close()
				rt.Close()
				t.Fatal("live instance lost its executable lease")
			}
			if err = instance.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatalf("non-flat-export executable mapping survived final close: base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
			}
		})
	}

}

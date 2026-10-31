//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/hex"
	"strconv"
	"testing"
)

var rv64PublicGCTypeSubtypingDeclarationsHex = []string{
	"0061736d0100000001a8808080000750005e7f005001005e7f0050005e6e0050005e63000050005e64010050005e7f015001055e7f01",
	"0061736d0100000001b3808080000650005f005001005f005001015f017f005001025f027f006300005001035f037f006400007e015001045f037f006401007e01",
	"0061736d0100000001af808080000650005f005001005f00500060016401016e5001026001640001646e5001036001630001640050010460016b016401",
	"0061736d010000000199808080000350005f016e005001005f016401005001015f026401007f00",
	"0061736d0100000001a0808080000250005f027f006400004e025001005f027f006402005001005f027f00640100",
	"0061736d0100000001bb80808000024e0250005f027f0064010050005f027e006400004e035001015f037e006400007f005001005f037f006401007f005001015f037e006403007f00",
}

func rv64PublicGCTypeSubtypingDeclarationsBytes(t testing.TB, variant uint8) []byte {
	t.Helper()
	if variant == 0 || int(variant) > len(rv64PublicGCTypeSubtypingDeclarationsHex) {
		t.Fatal("invalid declaration variant", variant)
	}
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingDeclarationsHex[variant-1])
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64PublicGCTypeSubtypingDeclarationsExecutionAndArtifact(t *testing.T) {

	for variant := uint8(1); int(variant) <= len(rv64PublicGCTypeSubtypingDeclarationsHex); variant++ {
		t.Run(strconv.Itoa(int(variant)), func(t *testing.T) {

			var compiledSet []*Compiled

			original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingDeclarationsBytes(t, variant))
			if err != nil {
				t.Fatal(err)
			}

			compiledSet = []*Compiled{original}

			for _, compiled := range compiledSet {
				defer compiled.Close()
				view := compiled.executionView()
				if len(view.code) != 0 || len(view.Entry) != 0 || len(view.InternalEntry) != 0 || view.validate() != nil {
					t.Fatal("invalid public declaration-only metadata")
				}
				rt := NewRuntime()
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
		})
	}
}

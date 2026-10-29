//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"testing"
)

const rv64I31OfficialGlobalInitializerHex = "0061736d01000000018580808000016000017f028a808080000103656e760167037f000382808080000100068880808000016c002300fb1c0b078780808000010367657400000a8c80808000018680808000002301fb1e0b"

func rv64I31OfficialGlobalInitializerBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31OfficialGlobalInitializerHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64I31OfficialGlobalInitializerCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64I31OfficialGlobalInitializerBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicI31OfficialGlobalInitializerExecutionAndArtifact(t *testing.T) {
	data := rv64I31OfficialGlobalInitializerBytes(t)
	wantSource := [sha256.Size]byte{0x02, 0x4b, 0x9a, 0x33, 0x4c, 0x9a, 0x7b, 0xb6, 0x93, 0x32, 0x43, 0xfa, 0x1e, 0xaf, 0x60, 0xed, 0x65, 0x33, 0x38, 0x03, 0x4b, 0xfa, 0xf9, 0xe5, 0x4f, 0xb2, 0x3c, 0xcc, 0x13, 0xc9, 0xad, 0x87}
	if len(data) != 88 || sha256.Sum256(data) != wantSource {
		t.Fatal("official i31 global-global initializer source identity changed")
	}
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64I31OfficialGlobalInitializerCompile(t, artifact)
			view := c.executionView()
			wantProduct := stagedGCI31ProductGlobalInitializer
			wantStaged := CoreFeatureTypedFunctionReferences | CoreFeatureGC
			if artifact {
				wantProduct = stagedGCI31ProductCore
				wantStaged = CoreFeatureGC
			}
			if !rv64I31OfficialGlobalInitializer(view) || !rv64I31Core(view) || railshotValidateCompiled(view) != nil || c.stagedGCI31Product() != wantProduct || c.stagedFeatures() != wantStaged {
				t.Fatal("missing exact retained official i31 global-global initializer witness")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			mutable := NewGlobalI32(42, true)
			if rejected, err := Instantiate(c, NewImports().Global("env", "g", mutable)); err == nil {
				_ = rejected.Close()
				t.Fatal("mutable imported global admitted for immutable i31 initializer")
			}
			_ = mutable.Close()
			cases := []struct {
				bits uint64
				want uint64
			}{
				{0x0000000040000000, 0x40000000},
				{0x00000000ffffffff, 0x7fffffff},
				{0xdeadbeef8000002a, 42},
			}
			for caseIndex, tc := range cases {
				g := NewGlobalI32(0, false)
				binary.LittleEndian.PutUint64(g.cell, tc.bits)
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "g", g))
				if in.gc != nil {
					t.Fatal("non-allocating official i31 global-global initializer provisioned a collector")
				}
				if caseIndex == len(cases)-1 {
					_ = c.Close() // A live instance retains its executable lease.
				}
				got, err := in.Invoke("get")
				if err != nil || !slices.Equal(got, []uint64{tc.want}) {
					t.Fatalf("bits=%#x get()=%v err=%v", tc.bits, got, err)
				}
				_ = in.Close()
				if g.owner.importers != 0 {
					t.Fatal("imported initializer global retained after instance close")
				}
				_ = g.Close()
			}
		})
	}
}

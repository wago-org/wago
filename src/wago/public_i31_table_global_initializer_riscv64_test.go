//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
)

const rv64I31OfficialTableGlobalInitializerHex = "0061736d010000000186808080000160017f017f028a808080000103656e760167037f000382808080000100048d80808000014000646c0103032300fb1c0b078780808000010367657400000a8e808080000188808080000020002500fb1e0b"

func rv64I31OfficialTableGlobalInitializerBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31OfficialTableGlobalInitializerHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64I31OfficialTableGlobalInitializerCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64I31OfficialTableGlobalInitializerBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicI31OfficialTableGlobalInitializerExecutionAndArtifact(t *testing.T) {
	data := rv64I31OfficialTableGlobalInitializerBytes(t)
	wantSource := [sha256.Size]byte{0x0a, 0x26, 0xe5, 0x0d, 0x6e, 0xc8, 0xcc, 0xba, 0xf1, 0xcc, 0x3a, 0x59, 0xfc, 0x2e, 0x1b, 0xe6, 0xdc, 0xa2, 0xc2, 0x22, 0x19, 0xba, 0x70, 0xca, 0x80, 0xa0, 0x14, 0x51, 0x88, 0x2b, 0xf0, 0xe4}
	if len(data) != 96 || sha256.Sum256(data) != wantSource {
		t.Fatal("official i31 table-global initializer source identity changed")
	}
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64I31OfficialTableGlobalInitializerCompile(t, artifact)
			view := c.executionView()
			wantProduct, wantStaged := stagedGCI31ProductTableGlobalInitializer, CoreFeatureTypedFunctionReferences|CoreFeatureGC
			if artifact {
				wantProduct = stagedGCI31ProductCore
			}
			if !rv64I31OfficialTableGlobalInitializer(view) || !rv64I31Core(view) || railshotValidateCompiled(view) != nil || c.stagedGCI31Product() != wantProduct || c.stagedFeatures() != wantStaged {
				t.Fatal("missing exact retained official i31 table-global initializer witness")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			mutable := NewGlobalI32(42, true)
			if rejected, err := Instantiate(c, NewImports().Global("env", "g", mutable)); err == nil {
				_ = rejected.Close()
				t.Fatal("mutable imported global admitted for immutable table initializer")
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
					t.Fatal("non-allocating official i31 table-global initializer provisioned a collector")
				}
				if caseIndex == len(cases)-1 {
					_ = c.Close() // A live instance retains its executable lease.
				}
				for index := uint64(0); index < 3; index++ {
					got, err := in.Invoke("get", 0xcafe123400000000|index)
					if err != nil || !slices.Equal(got, []uint64{tc.want}) {
						t.Fatalf("bits=%#x get(%d)=%v err=%v", tc.bits, index, got, err)
					}
				}
				if _, err := in.Invoke("get", I32(3)); err == nil || !strings.Contains(err.Error(), "table") {
					t.Fatalf("out-of-bounds table.get trap=%v", err)
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

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"
)

const rv64I31OfficialAnyGlobalHex = "0061736d01000000018a80808000026000027f7f60017f00038380808000020001069180808000026e0041d209fb1c0b6e0141ae2cfb1c0b079c80808000020b6765745f676c6f62616c7300000a7365745f676c6f62616c00010aa380808000029080808000002300fb176cfb1e2301fb176cfb1e0b8880808000002000fb1c24010b"

func rv64I31OfficialAnyGlobalBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31OfficialAnyGlobalHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64I31OfficialAnyGlobalCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64I31OfficialAnyGlobalBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicI31OfficialAnyGlobalExecutionAndArtifact(t *testing.T) {
	data := rv64I31OfficialAnyGlobalBytes(t)
	wantSource := [sha256.Size]byte{0x75, 0x7d, 0x32, 0x66, 0x61, 0x7e, 0xa9, 0x01, 0x22, 0x1f, 0xac, 0xbd, 0xa9, 0xb6, 0x60, 0xd6, 0xd1, 0xfd, 0x52, 0xad, 0xf4, 0x92, 0xff, 0xa8, 0x2b, 0xa0, 0x65, 0x8d, 0xc8, 0x46, 0xb2, 0x6d}
	if len(data) != 131 || sha256.Sum256(data) != wantSource {
		t.Fatal("official i31 anyref-global source identity changed")
	}
	for _, artifact := range []bool{false} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64I31OfficialAnyGlobalCompile(t, artifact)
			view := c.executionView()
			wantProduct, wantStaged := stagedGCI31ProductAnyGlobal, CoreFeatureTypedFunctionReferences|CoreFeatureGC
			if artifact {
				wantProduct, wantStaged = stagedGCI31ProductCore, CoreFeatureGC
			}
			if !rv64I31OfficialAnyGlobal(view) || !rv64I31Core(view) || railshotValidateCompiled(view) != nil || c.stagedGCI31Product() != wantProduct || c.stagedFeatures() != wantStaged {
				t.Fatal("missing exact retained official i31 anyref-global witness")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			if in.gc != nil {
				t.Fatal("non-allocating official i31 anyref-global product provisioned a collector")
			}
			if got, err := in.Invoke("get_globals"); err != nil || !slices.Equal(got, []uint64{1234, 5678}) {
				t.Fatalf("initial get_globals=%v err=%v", got, err)
			}
			if _, err := in.Invoke("set_global", 0xdeadbeef8000002a); err != nil {
				t.Fatal("dirty-upper set_global", err)
			}
			if got, err := in.Invoke("get_globals"); err != nil || !slices.Equal(got, []uint64{1234, 42}) {
				t.Fatalf("updated get_globals=%v err=%v", got, err)
			}
			writeGlobalObject(in.globalCells[0], ValAnyRef, 0)
			if _, err := in.Invoke("get_globals"); err == nil {
				t.Fatalf("null anyref cast trap=%v", err)
			} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapNullReference {
				t.Fatalf("null anyref cast trap=%v, want %v", err, TrapNullReference)
			}
			writeGlobalObject(in.globalCells[0], ValAnyRef, 2469)
			writeGlobalObject(in.globalCells[1], ValAnyRef, 2)
			if _, err := in.Invoke("get_globals"); err == nil {
				t.Fatalf("non-i31 anyref cast trap=%v", err)
			} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapCastFailure {
				t.Fatalf("non-i31 anyref cast trap=%v, want %v", err, TrapCastFailure)
			}
			writeGlobalObject(in.globalCells[1], ValAnyRef, 85)
			_ = c.Close() // A live instance retains its executable lease.
			if got, err := in.Invoke("get_globals"); err != nil || !slices.Equal(got, []uint64{1234, 42}) {
				t.Fatalf("post-compiled-close get_globals=%v err=%v", got, err)
			}
		})
	}
}

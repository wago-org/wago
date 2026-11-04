//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"slices"
	"testing"
)

const rv64GCArrayFixedOfficialHex = "0061736d0100000001b080808000095e7d005e7d01600001640060027f6400017d60017f017d60037f64017d017d60027f7d017d6001646a017f6000017f038880808000070203040506070806938080800001640000430000803f4300000040fb0800020b079d8080800004036e65770000036765740002077365745f6765740004036c656e00060afe8080800007908080800000430000803f4300000040fb0800020b89808080000020012000fb0b000b8880808000002000100010010b928080800000200120002002fb0e0120012000fb0b010b9b80808000002000430000803f43000000404300004040fb080103200110030b8680808000002000fb0f0b868080800000100010050b"

func rv64GCArrayFixedOfficialBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64GCArrayFixedOfficialHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func rv64GCArrayFixedOfficialCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64GCArrayFixedOfficialBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicGCArrayFixedOfficialExecutionAndArtifact(t *testing.T) {
	data := rv64GCArrayFixedOfficialBytes(t)
	wantSource := [sha256.Size]byte{0x6f, 0xf5, 0x95, 0x6b, 0x84, 0xb5, 0x03, 0x5d, 0xf8, 0xd3, 0x41, 0x9e, 0xdc, 0x8c, 0x67, 0x34, 0x8c, 0xff, 0xd0, 0x6d, 0x5a, 0x4c, 0xad, 0x86, 0xcf, 0xba, 0x56, 0xc4, 0x15, 0xac, 0xbf, 0x25}
	if len(data) != 268 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc array fixed official source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCArrayFixedOfficialCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64SourceContractValid(view) || err != nil {
				t.Fatalf("missing exact retained gc array fixed official witness: validation=%v products=%v/%v code=%d/%x entries=%v/%v", err, view.stagedGCStructProduct(), view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{
				{name: "default-gc"},
				{name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 96, VerifyAfterCollect: true}},
			} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(compiled)
					if err != nil {
						t.Fatal(err)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(profile.config))
					mod.Close()
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					state := in.pluginState.Load()
					stagedRoots := uint32(0)
					if state != nil {
						stagedRoots = state.gcGlobalRootCount
					}
					genericRoots := len(in.publicGCState().globalRoots)
					if stagedRoots+uint32(genericRoots) != 1 {
						t.Fatalf("fixed official array roots staged/generic = %d/%d", stagedRoots, genericRoots)
					}
					for _, tc := range []struct {
						index uint64
						want  uint64
					}{{0xaaaa000000000000, uint64(math.Float32bits(1))}, {0xbbbb000000000001, uint64(math.Float32bits(2))}} {
						if got, err := in.Invoke("get", tc.index); err != nil || !slices.Equal(got, []uint64{tc.want}) {
							t.Fatalf("get(%#x)=%v err=%v", tc.index, got, err)
						}
					}
					replacement := uint64(0xcccc0000ffc12345)
					if got, err := in.Invoke("set_get", 0xdddd000000000002, replacement); err != nil || !slices.Equal(got, []uint64{0xffc12345}) {
						t.Fatalf("set_get()=%v err=%v", got, err)
					}
					if got, err := in.Invoke("len"); err != nil || !slices.Equal(got, []uint64{2}) {
						t.Fatalf("len()=%v err=%v", got, err)
					}
					if _, err = in.Invoke("get", 2); err == nil {
						t.Fatal("out-of-bounds get succeeded")
					}
					if _, err = in.Invoke("set_get", 3, 0); err == nil {
						t.Fatal("out-of-bounds set succeeded")
					}
					raw, err := in.Invoke("new")
					if err != nil || len(raw) != 1 || raw[0] == 0 {
						t.Fatalf("new()=%v err=%v", raw, err)
					}
					if err = in.ReleaseGCRef(ValueOf(ValAnyRef, raw[0]).GCRef()); err != nil {
						t.Fatal(err)
					}
					if err = in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

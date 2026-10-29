//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"slices"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64GCArrayDefaultOfficialHex = "0061736d0100000001b080808000095e7d005e7d01600001640060027f6400017d60017f017d60037f64017d017d60027f7d017d6001646a017f6000017f038880808000070203040506070806988080800002640000430000803f4103fb06000b6400004103fb07000b079d8080800004036e65770000036765740002077365745f6765740004036c656e00060ae780808000078780808000004103fb07000b89808080000020012000fb0b000b8880808000002000100010010b928080800000200120002002fb0e0120012000fb0b010b8d808080000020004103fb0701200110030b8680808000002000fb0f0b868080800000100010050b"

func rv64GCArrayDefaultOfficialBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64GCArrayDefaultOfficialHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func rv64GCArrayDefaultOfficialCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64GCArrayDefaultOfficialBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original

	return c
}

func TestRV64PublicGCArrayDefaultOfficialExecutionAndArtifact(t *testing.T) {
	data := rv64GCArrayDefaultOfficialBytes(t)
	wantSource := [sha256.Size]byte{0xdf, 0xf1, 0x8b, 0xcf, 0x6b, 0x1e, 0xd6, 0xfd, 0xb6, 0xae, 0x63, 0x69, 0x2b, 0xaa, 0x8e, 0x64, 0x9e, 0x22, 0x79, 0x4d, 0xe7, 0xf4, 0xdb, 0xf3, 0xbc, 0x76, 0xe0, 0xf2, 0xb0, 0xf2, 0x88, 0x98}
	if len(data) != 250 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc array default official source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCArrayDefaultOfficialCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64SourceContractValid(view) || err != nil {
				t.Fatalf("missing exact retained gc array default official witness: validation=%v products=%v/%v code=%d/%x entries=%v/%v", err, view.stagedGCStructProduct(), view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
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
					var roots []gcGlobalRootMapping
					if state != nil {
						roots = append(roots, state.gcGlobalRoots[:state.gcGlobalRootCount]...)
					}
					public := in.publicGCState()
					roots = append(roots, public.globalRoots...)
					if len(roots) != 2 {
						t.Fatalf("default official array roots staged/generic = %d/%d", len(roots)-len(public.globalRoots), len(public.globalRoots))
					}
					var seen [2]bool
					for _, mapping := range roots {
						if mapping.GlobalIndex >= uint32(len(seen)) || seen[mapping.GlobalIndex] {
							t.Fatalf("default official duplicate/out-of-range global root %+v", mapping)
						}
						seen[mapping.GlobalIndex] = true
						ref := gc.Ref(uint32(readGlobalObject(in.globalCells[mapping.GlobalIndex], ValAnyRef)))
						rooted, err := in.gc.CheckedGlobalSlot(mapping.SlotIndex)
						if err != nil || rooted != ref || !rooted.IsObj() {
							t.Fatalf("root mapping %d = %v, %v; want %v", mapping.GlobalIndex, rooted, err, ref)
						}
						for j := uint32(0); j < 3; j++ {
							value, err := in.gc.ArrayGet(ref, j)
							want := uint64(0)
							if mapping.GlobalIndex == 0 {
								want = uint64(math.Float32bits(1))
							}
							if err != nil || value.Bits != want {
								t.Fatalf("global %d element %d = %#x, %v; want %#x", mapping.GlobalIndex, j, value.Bits, err, want)
							}
						}
					}
					for _, tc := range []struct {
						index uint64
						want  uint64
					}{{0xaaaa000000000000, 0}, {0xbbbb000000000002, 0}} {
						if got, err := in.Invoke("get", tc.index); err != nil || !slices.Equal(got, []uint64{tc.want}) {
							t.Fatalf("get(%#x)=%v err=%v", tc.index, got, err)
						}
					}
					replacement := uint64(0xcccc0000ffc12345)
					if got, err := in.Invoke("set_get", 0xdddd000000000002, replacement); err != nil || !slices.Equal(got, []uint64{0xffc12345}) {
						t.Fatalf("set_get()=%v err=%v", got, err)
					}
					if got, err := in.Invoke("len"); err != nil || !slices.Equal(got, []uint64{3}) {
						t.Fatalf("len()=%v err=%v", got, err)
					}
					if _, err = in.Invoke("get", 3); err == nil {
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
					if stats := in.gc.Stats(); stats.LiveObjects != 2 {
						t.Fatalf("post-action default global stats = %+v", stats)
					}
				})
			}
		})
	}
}

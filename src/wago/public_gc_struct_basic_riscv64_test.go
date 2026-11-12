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

const rv64PublicGCStructBasicHex = "0061736d0100000001a380808000065f037d007d017d006000016e60016400017d6000017d600264007d017d60017d017d038e808080000d01020302030203020304050405069e8080800002640000430000803f43000000404300004040fb00000b640000fb01000b07cb8080800007036e65770000076765745f305f300002096765745f7665635f300004076765745f305f790006096765745f7665635f790008097365745f6765745f79000a097365745f6765745f31000c0ab5818080000d858080800000fb01000b8880808000002000fb0200000b878080800000fb010010010b8880808000002000fb0200000b878080800000fb010010030b8880808000002000fb0200010b878080800000fb010010050b8880808000002000fb0200010b878080800000fb010010070b90808080000020002001fb0500012000fb0200010b898080800000fb0100200010090b90808080000020002001fb0500012000fb0200010b898080800000fb01002000100b0b"

func rv64PublicGCStructBasicBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCStructBasicHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCStructBasicCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCStructBasicBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func TestRV64PublicGCStructBasicExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCStructBasicBytes(t)
	if len(data) != 373 || sha256.Sum256(data) != [sha256.Size]byte{0xa4, 0x69, 0xba, 0x81, 0xd1, 0x4d, 0xdf, 0x21, 0x10, 0x0f, 0x10, 0x0c, 0xb6, 0x7d, 0x75, 0x94, 0x2b, 0xc0, 0x04, 0x5b, 0x9e, 0xb0, 0x9e, 0x6c, 0x4f, 0xc9, 0x05, 0x2a, 0xc5, 0xab, 0x6c, 0x83} {
		t.Fatal("gc/struct basic source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCStructBasicCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) {
				t.Fatalf("missing exact basic witness: code=%d/%x entries=%v/%v product=%v", len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.stagedGCStructProduct())
			}
			rt := NewRuntime()
			mod, err := rt.Module(compiled)
			if err != nil {
				rt.Close()
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 96, VerifyAfterCollect: true}))
			mod.Close()
			if err != nil {
				rt.Close()
				t.Fatal(err)
			}
			var roots []gcGlobalRootMapping
			state := in.pluginState.Load()
			if state != nil {
				roots = append(roots, state.gcGlobalRoots[:state.gcGlobalRootCount]...)
			}
			stagedRoots := len(roots)
			public := in.publicGCState()
			roots = append(roots, public.globalRoots...)
			runtimeRoots := in.c.needsRuntimeGCCollectorDomain()
			if len(roots) != 2 || runtimeRoots && (stagedRoots != 0 || len(public.globalRoots) != 2) || !runtimeRoots && (stagedRoots != 2 || len(public.globalRoots) != 0) {
				t.Fatalf("gc/struct basic global roots staged/generic=%d/%d, want exact two-slot representation", stagedRoots, len(public.globalRoots))
			}
			var seen [2]bool
			for _, mapping := range roots {
				if mapping.GlobalIndex >= uint32(len(seen)) || seen[mapping.GlobalIndex] {
					t.Fatalf("duplicate/out-of-range gc/struct basic root %+v", mapping)
				}
				seen[mapping.GlobalIndex] = true
				want := uint32(readGlobalObject(in.globalCells[mapping.GlobalIndex], ValAnyRef))
				rooted, err := in.gc.CheckedGlobalSlot(mapping.SlotIndex)
				if err != nil || uint32(rooted) != want || !rooted.IsObj() {
					t.Fatalf("gc/struct basic root %d=%v,%v want object %d", mapping.GlobalIndex, rooted, err, want)
				}
			}
			for _, name := range []string{"get_0_0", "get_vec_0", "get_0_y", "get_vec_y"} {
				if got, err := in.Invoke(name); err != nil || !slices.Equal(got, []uint64{0}) {
					t.Fatalf("%s=%v,%v want [0]", name, got, err)
				}
			}
			seven := uint64(math.Float32bits(7))
			for _, name := range []string{"set_get_y", "set_get_1"} {
				if got, err := in.Invoke(name, 0xffff000000000000|seven); err != nil || !slices.Equal(got, []uint64{seven}) {
					t.Fatalf("%s=%v,%v want [%#x]", name, got, err, seven)
				}
			}
			raw, err := in.Invoke("new")
			if err != nil || len(raw) != 1 || raw[0] == 0 || raw[0]>>32 == 0 {
				t.Fatalf("new=%v,%v want opaque non-null token", raw, err)
			}
			if err := in.ReleaseGCRef(ValueOf(ValAnyRef, raw[0]).GCRef()); err != nil {
				t.Fatal(err)
			}
			values, err := in.InvokeValues(context.Background(), "new")
			if err != nil || len(values) != 1 || values[0].Type() != ValAnyRef || values[0].GCRef().IsNull() {
				t.Fatalf("typed new=%v,%v", values, err)
			}
			if err := in.ReleaseGCRef(values[0].GCRef()); err != nil {
				t.Fatal(err)
			}
			if err := in.CollectGC(); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"get_0_0", "get_vec_y"} {
				if got, err := in.Invoke(name); err != nil || !slices.Equal(got, []uint64{0}) {
					t.Fatalf("post-collection %s=%v,%v want [0]", name, got, err)
				}
			}
			if stats := in.gc.Stats(); stats.LiveObjects > 3 {
				t.Fatalf("post-release collector stats=%+v", stats)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			rt.Close()
		})
	}
}

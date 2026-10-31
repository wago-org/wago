//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64PublicGCArrayInitDataHex = "0061736d01000000019880808000065e78005e78015e770160017f017f60000060037f7f7f0003878080800006030304050504069e8080800003640000410a410cfb06000b640101410cfb07010b6402004106fb07020b07f080808000060d61727261795f6765745f6e746800001161727261795f6765745f6e74685f69313600011461727261795f696e69745f646174612d6e756c6c00020f61727261795f696e69745f6461746100031361727261795f696e69745f646174615f69313600040964726f705f7365677300050c8180808000010ae0808080000689808080000023012000fb0d010b89808080000023022000fb0d020b8e8080800000d001410041004100fb1201000b8e80808000002301200020012002fb1201000b8e80808000002302200020012002fb1202000b858080800000fc09000b0b8f8080800001010c6162636465666768696a6b6c"

func rv64PublicGCArrayInitDataBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayInitDataHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayInitDataCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCArrayInitDataBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func TestRV64PublicGCArrayInitDataExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayInitDataBytes(t)
	wantSource := [sha256.Size]byte{0xc1, 0x7d, 0xa5, 0x6e, 0xd5, 0xc6, 0x50, 0x83, 0xee, 0x20, 0x02, 0x37, 0x38, 0xcc, 0x5d, 0x9a, 0x36, 0xd1, 0xe3, 0x01, 0xd2, 0xf0, 0x6f, 0x23, 0xe2, 0x64, 0x5d, 0x6e, 0xc8, 0xa9, 0xca, 0x77}
	if len(data) != 335 || sha256.Sum256(data) != wantSource {
		t.Fatal("array.init_data source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayInitDataCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64SourceContractValid(view) || err != nil {
				t.Fatalf("missing exact array.init_data witness: validation=%v product=%v code=%d/%x entries=%v/%v globals=%#v", err, view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.memoryDir.gcArrayGlobals)
			}
			if params, results, err := railshotImportedCallSlots(view, 3, 0); err != nil || params != 3 || results != 0 {
				t.Fatalf("array.init_data call arena=%d/%d,%v, want 3/0", params, results, err)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 224, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
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
					if len(in.serArgs) < 3*8 {
						t.Fatalf("array.init_data argument arena=%d bytes, want at least 24", len(in.serArgs))
					}
					if len(in.passiveDataDesc) != 16 || binary.LittleEndian.Uint32(in.passiveDataDesc[8:]) != 12 {
						t.Fatalf("passive descriptor=%x", in.passiveDataDesc)
					}
					state := in.pluginState.Load()
					var roots []gcGlobalRootMapping
					if state != nil {
						roots = append(roots, state.gcGlobalRoots[:state.gcGlobalRootCount]...)
					}
					roots = append(roots, in.publicGCState().globalRoots...)
					if len(roots) != 3 {
						t.Fatalf("array.init_data global roots=%d, want 3", len(roots))
					}
					var globalRefs [3]gc.Ref
					for _, mapping := range roots {
						ref := gc.Ref(uint32(readGlobalObject(in.globalCells[mapping.GlobalIndex], ValAnyRef)))
						rooted, slotErr := in.gc.CheckedGlobalSlot(mapping.SlotIndex)
						if slotErr != nil || rooted != ref || !rooted.IsObj() {
							t.Fatalf("global root %d=%v,%v want %v", mapping.GlobalIndex, rooted, slotErr, ref)
						}
						globalRefs[mapping.GlobalIndex] = ref
					}
					for _, call := range []struct {
						name string
						args []uint64
					}{{"array_init_data-null", nil}, {"array_init_data", []uint64{13, 0, 0}}, {"array_init_data", []uint64{0, 13, 0}}, {"array_init_data", []uint64{0, 0, 13}}, {"array_init_data_i16", []uint64{0, 0, 7}}} {
						if _, err = in.Invoke(call.name, call.args...); err == nil {
							t.Fatalf("trapping %s%v returned normally", call.name, call.args)
						}
					}
					for _, call := range []struct {
						name string
						args []uint64
					}{{"array_init_data", []uint64{0xffff00000000000c, 0, 0}}, {"array_init_data", []uint64{0, 0xffff00000000000c, 0}}, {"array_init_data_i16", []uint64{0, 0xffff000000000006, 0}}} {
						if got, err := in.Invoke(call.name, call.args...); err != nil || len(got) != 0 {
							t.Fatalf("zero-length %s%v=%v,%v", call.name, call.args, got, err)
						}
					}
					if _, err = in.Invoke("array_init_data", 0, 11, 2); err == nil {
						t.Fatal("short source returned normally")
					}
					if got, err := in.Invoke("array_get_nth", 0); err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatalf("trapping init changed destination: %v,%v", got, err)
					}
					if got, err := in.Invoke("array_init_data", 0xaaaa000000000004, 0xbbbb000000000002, 0xcccc000000000002); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper array.init_data=%v,%v", got, err)
					}
					for index, want := range map[uint64]uint64{3: 0, 4: 99, 5: 100, 6: 0} {
						if got, err := in.Invoke("array_get_nth", index); err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatalf("array_get_nth(%d)=%v,%v want %d", index, got, err, want)
						}
					}
					if got, err := in.Invoke("array_init_data_i16", 0xaaaa000000000002, 0xbbbb000000000005, 0xcccc000000000002); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper array.init_data_i16=%v,%v", got, err)
					}
					for index, want := range map[uint64]uint64{1: 0, 2: 0x6766, 3: 0x6968, 4: 0} {
						if got, err := in.Invoke("array_get_nth_i16", index); err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatalf("array_get_nth_i16(%d)=%v,%v want %d", index, got, err, want)
						}
					}
					if err = in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 3 {
						t.Fatalf("live objects=%d, want three rooted arrays", live)
					}
					if got, err := in.Invoke("drop_segs"); err != nil || len(got) != 0 {
						t.Fatalf("drop segment=%v,%v", got, err)
					}
					if binary.LittleEndian.Uint32(in.passiveDataDesc[8:]) != 0 {
						t.Fatalf("dropped descriptor=%x", in.passiveDataDesc)
					}
					if _, err = in.Invoke("array_init_data", 0, 0, 0); err != nil {
						t.Fatalf("zero length after drop: %v", err)
					}
					if _, err = in.Invoke("array_init_data", 0, 0, 1); err == nil {
						t.Fatal("non-zero init after drop returned normally")
					}
				})
			}
		})
	}
}

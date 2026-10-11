//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64PublicGCArrayInitElemHex = "0061736d01000000019580808000056000005e6400005e700160017f0060037f7f7f000386808080000500030004000484808080000170000106958080800002640100d200410cfb06010b640200410cfb07020b07c780808000040e61727261795f63616c6c5f6e746800011461727261795f696e69745f656c656d2d6e756c6c00020f61727261795f696e69745f656c656d00030964726f705f7365677300040990808080000101000c0000000000000000000000000acf80808000058280808000000b928080800000410023012000fb0b02260041001100000b8e8080800000d002410041004100fb1302000b8e80808000002301200020012002fb1302000b858080800000fc0d000b"

func rv64PublicGCArrayInitElemBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayInitElemHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayInitElemCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-array-init-elem.wago")
	if mode == "load" {
		if !artifact {
			t.Skip("compiler-free runner selected artifact case")
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = loaded.Close() })
		return loaded
	}
	original, err := rv64I31Config().Compile(rv64PublicGCArrayInitElemBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	if !artifact && mode != "prepare" {
		return original
	}
	blob, err := original.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err = os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, blob, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !artifact {
		return original
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	return loaded
}

func TestRV64PublicGCArrayInitElemExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayInitElemBytes(t)
	wantSource := [sha256.Size]byte{0x77, 0x15, 0x3c, 0xc9, 0x16, 0x6a, 0x1b, 0x88, 0xe5, 0x64, 0xa9, 0x3e, 0x47, 0x3e, 0x2d, 0x4d, 0x31, 0x97, 0x92, 0x88, 0xac, 0x4b, 0x82, 0xb9, 0xb0, 0x03, 0x89, 0x11, 0xcd, 0x15, 0x98, 0x3b}
	if len(data) != 268 || sha256.Sum256(data) != wantSource {
		t.Fatal("array.init_elem source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayInitElemCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64GCArrayInitElem(view) || !rv64GCArrayInitElemExecutionCode(view) || err != nil {
				t.Fatalf("missing exact array.init_elem witness: validation=%v product=%v code=%d/%x entries=%v/%v", err, view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			if params, results, err := railshotImportedCallSlots(view, 3, 0); err != nil || params != 6 || results != 0 {
				t.Fatalf("array.init_elem call arena=%d/%d,%v, want 6/0", params, results, err)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{
				{name: "default-gc"},
				{name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 224, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}},
			} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
					defer rt.Close()
					mod, err := rt.Module(compiled)
					if err != nil {
						t.Fatal(err)
					}
					if !rv64GCArrayInitElem(mod.c) {
						t.Fatalf("frozen array.init_elem witness changed: product=%v globals=%#v entries=%v/%v passive=%#v", mod.c.stagedGCArrayProduct(), mod.c.memoryDir.gcArrayGlobals, mod.c.Entry, mod.c.InternalEntry, mod.c.passiveElems)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(profile.config))
					mod.Close()
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if len(in.serArgs) < 6*8 {
						t.Fatalf("array.init_elem argument arena=%d bytes, want at least 48", len(in.serArgs))
					}

					state := in.pluginState.Load()
					var roots []gcGlobalRootMapping
					if state != nil {
						roots = append(roots, state.gcGlobalRoots[:state.gcGlobalRootCount]...)
					}
					roots = append(roots, in.publicGCState().globalRoots...)
					if len(roots) != 2 {
						t.Fatalf("array.init_elem global roots=%d, want 2", len(roots))
					}
					var globalRefs [2]gc.Ref
					for _, mapping := range roots {
						if mapping.GlobalIndex >= uint32(len(globalRefs)) || !globalRefs[mapping.GlobalIndex].IsNull() {
							t.Fatalf("duplicate/out-of-range global root %+v", mapping)
						}
						ref := gc.Ref(uint32(readGlobalObject(in.globalCells[mapping.GlobalIndex], ValAnyRef)))
						rooted, slotErr := in.gc.CheckedGlobalSlot(mapping.SlotIndex)
						if slotErr != nil || rooted != ref || !rooted.IsObj() {
							t.Fatalf("global root %d=%v,%v want %v", mapping.GlobalIndex, rooted, slotErr, ref)
						}
						globalRefs[mapping.GlobalIndex] = ref
						if length, lengthErr := in.gc.ArrayLen(ref); lengthErr != nil || length != 12 {
							t.Fatalf("global array %d length=%d,%v", mapping.GlobalIndex, length, lengthErr)
						}
					}
					if globalRefs[0].IsNull() || globalRefs[1].IsNull() {
						t.Fatalf("missing rooted globals %v", globalRefs)
					}
					descPtr := in.jm.PassiveElemPtr()
					if descPtr == 0 {
						t.Fatal("passive element descriptor is unavailable")
					}
					desc := unsafe.Slice((*byte)(offHeapPtr(descPtr)), 16)
					if got := binary.LittleEndian.Uint32(desc[8:]); got != 12 {
						t.Fatalf("passive element length=%d, want 12", got)
					}
					if _, err = in.Invoke("array_init_elem", 0, 0, 0); err != nil {
						t.Fatalf("initial zero-length array.init_elem: %v", err)
					}
					if _, err = in.Invoke("array_init_elem-null"); err == nil {
						t.Fatal("array.init_elem on null returned normally")
					}
					for _, args := range [][]uint64{{13, 0, 0}, {0, 13, 0}, {0, 0, 13}, {0xffff000000000000, 0xffff000000000000, 0xffff00000000000d}} {
						if _, err = in.Invoke("array_init_elem", args...); err == nil {
							t.Fatalf("out-of-bounds array.init_elem%v returned normally", args)
						}
					}
					for _, args := range [][]uint64{{0xffff00000000000c, 0, 0}, {0, 0xffff00000000000c, 0}} {
						if got, invokeErr := in.Invoke("array_init_elem", args...); invokeErr != nil || len(got) != 0 {
							t.Fatalf("zero-length array.init_elem%v=%v,%v", args, got, invokeErr)
						}
					}
					for _, index := range []uint64{0, 5, 11, 12} {
						if _, err = in.Invoke("array_call_nth", index); err == nil {
							t.Fatalf("initial call at %d returned normally", index)
						}
					}
					before, err := in.gc.ArrayGet(globalRefs[1], 0)
					if err != nil || before.Bits != 0 {
						t.Fatalf("destination before trapping init=%+v,%v", before, err)
					}
					if _, err = in.Invoke("array_init_elem", 0, 11, 2); err == nil {
						t.Fatal("short source returned normally")
					}
					after, err := in.gc.ArrayGet(globalRefs[1], 0)
					if err != nil || after.Bits != before.Bits {
						t.Fatalf("trapping init changed destination: before=%+v after=%+v err=%v", before, after, err)
					}
					if got, err := in.Invoke("array_init_elem", 0xaaaa000000000002, 0xbbbb000000000003, 0xcccc000000000002); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper array.init_elem=%v,%v", got, err)
					}
					for _, index := range []uint64{0xaaaa000000000002, 0xbbbb000000000003} {
						if got, err := in.Invoke("array_call_nth", index); err != nil || len(got) != 0 {
							t.Fatalf("initialized call(%#x)=%v,%v", index, got, err)
						}
					}
					for _, index := range []uint64{1, 4} {
						if _, err = in.Invoke("array_call_nth", index); err == nil {
							t.Fatalf("neighbor call at %d returned normally", index)
						}
					}
					if err = in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 2 {
						t.Fatalf("live objects=%d, want two rooted arrays", live)
					}
					if _, err = in.Invoke("array_call_nth", 3); err != nil {
						t.Fatalf("initialized function reference after collection: %v", err)
					}
					if got, err := in.Invoke("drop_segs"); err != nil || len(got) != 0 {
						t.Fatalf("drop segment=%v,%v", got, err)
					}
					if got := binary.LittleEndian.Uint32(desc[8:]); got != 0 {
						t.Fatalf("dropped segment length=%d", got)
					}
					if _, err = in.Invoke("array_init_elem", 0, 0, 0); err != nil {
						t.Fatalf("zero length after drop: %v", err)
					}
					if _, err = in.Invoke("array_init_elem", 0, 0, 1); err == nil {
						t.Fatal("non-zero init after drop returned normally")
					}
				})
			}
		})
	}
}

func TestRV64PublicGCArrayInitElemAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCArrayInitElemCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[2]++; v.InternalEntry[2]++ },
		"function":   func(v *Compiled) { v.Funcs[3].Params[2] = ValI64 },
		"type":       func(v *Compiled) { v.Types[2].Array.Mutable = false },
		"type-key":   func(v *Compiled) { v.FuncTypeID[3] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[2].ElemSize++ },
		"table":      func(v *Compiled) { v.TableSize++ },
		"global":     func(v *Compiled) { v.Globals[1].Mutable = true },
		"element":    func(v *Compiled) { v.passiveElems[0].Values[7].FuncIndex = 1 },
		"initializer": func(v *Compiled) {
			v.memoryDir.gcArrayGlobals[0].Bits[0]++
		},
		"export":  func(v *Compiled) { delete(v.Exports, "array_init_elem"); v.Exports["init"] = 3 },
		"feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			if candidate.memoryDir != nil {
				dir := *candidate.memoryDir
				dir.gcArrayGlobals = slices.Clone(dir.gcArrayGlobals)
				for i := range dir.gcArrayGlobals {
					dir.gcArrayGlobals[i].Bits = slices.Clone(dir.gcArrayGlobals[i].Bits)
				}
				candidate.memoryDir = &dir
			}
			mutate(candidate)
			if rv64GCArrayInitElem(candidate) {
				t.Fatal("hostile mutation retained array.init_elem witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCArrayInitElemCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCArrayInitElemBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free array.init_elem path used source compilation", err)
	}
}

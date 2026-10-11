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

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

const rv64PublicGCArrayReferenceHex = "0061736d0100000001cd808080000f5e78005e6400005e6400015e6300005e6e0160000164016000016403600001640460037f7f6401017f60027f7f017f60047f7f64027f017f60037f7f7f017f6001646a017f6000017f600000038c808080000b0505060708090a0b0c0d0e07b88080800006036e657700000c6e65772d6f766572666c6f770001036765740005077365745f6765740007036c656e00090964726f705f73656773000a099680808000010564000241074103fb06000b41014102fb0800020b0abf818080000b8a808080000041004102fb0a01000b928080800000418080808078418080808078fb0a01000b8a808080000041004102fb0a03000b8a808080000041004102fb0a04000b8e808080000020022000fb0b012001fb0d000b8a808080000020002001100010040b9c80808000002002200020022003fb0b02fb0e0220022000fb0b022001fb0d000b9280808000002000200141004102fb0a0200200210060b8680808000002000fb0f0b868080800000100010080b858080800000fc0d000b"

func rv64PublicGCArrayReferenceBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayReferenceHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayReferenceCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-array-reference.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCArrayReferenceBytes(t))
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

func TestRV64PublicGCArrayReferenceExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayReferenceBytes(t)
	wantSource := [sha256.Size]byte{0x19, 0x17, 0x8a, 0x5d, 0xb9, 0xc6, 0xde, 0xd4, 0x1e, 0x18, 0x5a, 0x94, 0x22, 0xc5, 0x58, 0xa6, 0x5d, 0x4b, 0xc1, 0xf1, 0x1e, 0x7b, 0x0d, 0xf1, 0x1a, 0x77, 0x62, 0x26, 0xf2, 0x28, 0x12, 0xa9}
	if len(data) != 396 || sha256.Sum256(data) != wantSource {
		t.Fatal("reference array source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayReferenceCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64GCArrayReference(view) || !rv64GCArrayReferenceExecutionCode(view) || err != nil {
				t.Fatalf("missing reference array witness: %v", err)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default"}, {name: "stress", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 96, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
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
					state := in.existingGCArrayElementState()
					if state == nil || state.Count != 2 || len(state.Descriptor) != 16 || binary.LittleEndian.Uint32(state.Descriptor[8:]) != 2 {
						t.Fatalf("passive element state=%+v", state)
					}
					raw, err := in.Invoke("new")
					if err != nil || len(raw) != 1 || raw[0] == 0 {
						t.Fatalf("new=%v err=%v", raw, err)
					}
					exact, owner, _, ok := in.refStore.gcRefExactType(raw[0])
					if !ok || owner != in || exact.Kind != ValueTypeReference || !exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.TypeIndex != 1 {
						t.Fatalf("reference token type=%#v owner=%p ok=%v", exact, owner, ok)
					}
					if err = in.ReleaseGCRef(ValueOf(ValAnyRef, raw[0]).GCRef()); err != nil {
						t.Fatal(err)
					}
					for _, action := range []struct {
						name       string
						args, want []uint64
					}{
						{name: "get", args: []uint64{0xaaaa000000000000, 0xbbbb000000000000}, want: []uint64{7}},
						{name: "get", args: []uint64{0xaaaa000000000001, 0xbbbb000000000000}, want: []uint64{1}},
						{name: "set_get", args: []uint64{0xaaaa000000000000, 0xbbbb000000000001, 0xcccc000000000001}, want: []uint64{2}},
						{name: "len", want: []uint64{2}},
					} {
						got, err := in.Invoke(action.name, action.args...)
						if err != nil || !slices.Equal(got, action.want) {
							t.Fatalf("%s%v=%v err=%v", action.name, action.args, got, err)
						}
					}
					before := in.gc.Stats()
					if _, err = in.Invoke("new-overflow"); err == nil {
						t.Fatal("overflowing array.new_elem succeeded")
					}
					after := in.gc.Stats()
					if after.Allocations != before.Allocations || after.LiveObjects != before.LiveObjects {
						t.Fatalf("overflow mutated collector: before=%+v after=%+v", before, after)
					}
					if _, err = in.Invoke("get", 10, 0); err == nil {
						t.Fatal("out-of-bounds reference get succeeded")
					}
					if _, err = in.Invoke("set_get", 10, 0, 0); err == nil {
						t.Fatal("out-of-bounds reference set succeeded")
					}
					if got, err := in.Invoke("drop_segs"); err != nil || len(got) != 0 {
						t.Fatalf("drop=%v err=%v", got, err)
					}
					if binary.LittleEndian.Uint32(state.Descriptor[8:]) != 0 {
						t.Fatalf("dropped descriptor=%x", state.Descriptor)
					}
					for i := uint32(0); i < state.Count; i++ {
						if rooted, err := in.gc.CheckedTableSlot(state.Slots[i]); err != nil || !rooted.IsNull() {
							t.Fatalf("dropped root %d=%v err=%v", i, rooted, err)
						}
					}
					if _, err = in.Invoke("drop_segs"); err != nil {
						t.Fatal("repeated drop", err)
					}
					if _, err = in.Invoke("new"); err == nil {
						t.Fatal("array.new_elem after drop succeeded")
					}
				})
			}
		})
	}
}

func TestRV64PublicGCArrayReferenceCloseDropsOnlyOwnedSegmentRoots(t *testing.T) {
	// Exercise the decoded artifact form: its generic-array execution marker
	// selects the shared Runtime collector while the exact retained witness keeps
	// this specialized native lowering admitted.
	compiled := rv64PublicGCArrayReferenceCompile(t, true)
	store := newReferenceStore(false)
	defer store.closeRuntime()
	config := GCConfig{VerifyAfterCollect: true, StressBarriers: true}
	first, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		t.Fatal(err)
	}
	second, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	defer second.Close()
	if first.gc == nil || first.gc != second.gc || !store.ownsGCCollector(first.gc) {
		t.Fatalf("reference array collector domains first=%p second=%p store-owned=%t needs=%t safe=%t", first.gc, second.gc, store.ownsGCCollector(first.gc), compiled.needsRuntimeGCCollectorDomain(), compiled.sharedGCPersistentDomainSafe())
	}
	firstState, secondState := first.existingGCArrayElementState(), second.existingGCArrayElementState()
	if firstState == nil || secondState == nil || firstState == secondState || firstState.Count != 2 || secondState.Count != 2 {
		t.Fatalf("element states = %p/%+v and %p/%+v", firstState, firstState, secondState, secondState)
	}
	collector := second.gc
	if got := collector.Stats().LiveObjects; got != 4 {
		t.Fatalf("initial live objects = %d, want 4", got)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if !first.resourcesClosed || first.existingGCArrayElementState() != nil {
		t.Fatalf("first physical teardown retained element state: closed=%t state=%p", first.resourcesClosed, first.existingGCArrayElementState())
	}
	for i := uint32(0); i < firstState.Count; i++ {
		rooted, slotErr := collector.CheckedTableSlot(firstState.Slots[i])
		if slotErr != nil || !rooted.IsNull() {
			t.Fatalf("closed instance root %d = %v, %v", i, rooted, slotErr)
		}
	}
	for i := uint32(0); i < secondState.Count; i++ {
		rooted, slotErr := collector.CheckedTableSlot(secondState.Slots[i])
		if slotErr != nil || rooted.IsNull() {
			t.Fatalf("surviving instance root %d = %v, %v", i, rooted, slotErr)
		}
	}
	if err = collector.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if got := collector.Stats().LiveObjects; got != 2 {
		t.Fatalf("live objects after closed-instance collection = %d, want 2", got)
	}
	for i, want := range []uint64{7, 1} {
		rooted, slotErr := collector.CheckedTableSlot(secondState.Slots[i])
		if slotErr != nil || rooted.IsNull() {
			t.Fatalf("surviving instance root %d after collection = %v, %v", i, rooted, slotErr)
		}
		value, valueErr := collector.ArrayGet(rooted, 0)
		if valueErr != nil || value.Bits != want {
			t.Fatalf("surviving element %d = %+v, %v; want %d", i, value, valueErr, want)
		}
	}
}

func TestRV64PublicGCArrayReferenceAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCArrayReferenceCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":     func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":    func(v *Compiled) { v.Entry[7]++; v.InternalEntry[7]++ },
		"function": func(v *Compiled) { v.Funcs[6].Params[2] = ValI64 },
		"type":     func(v *Compiled) { v.Types[2].Array.Mutable = false },
		"type-key": func(v *Compiled) { v.FuncTypeID[8] ^= 1 },
		"descriptor": func(v *Compiled) {
			v.GCTypeDescs[1].ElemSize++
		},
		"element-mode": func(v *Compiled) { v.passiveElems[0].Mode = ElemModeDeclarative },
		"initializer":  func(v *Compiled) { v.memoryDir.gcArrayElement.Values[0].Bits[0]++ },
		"export":       func(v *Compiled) { delete(v.Exports, "new"); v.Exports["make"] = 0 },
		"feature":      func(v *Compiled) { v.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			if candidate.memoryDir != nil && candidate.memoryDir.gcArrayElement != nil {
				dir := *candidate.memoryDir
				candidate.memoryDir = &dir
				init := *candidate.memoryDir.gcArrayElement
				init.Values = slices.Clone(init.Values)
				for i := range init.Values {
					init.Values[i].Bits = slices.Clone(init.Values[i].Bits)
				}
				candidate.memoryDir.gcArrayElement = &init
			}
			mutate(candidate)
			if rv64GCArrayReference(candidate) {
				t.Fatal("hostile mutation retained reference witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCArrayReferenceCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCArrayReferenceBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free reference array path used source compilation", err)
	}
}

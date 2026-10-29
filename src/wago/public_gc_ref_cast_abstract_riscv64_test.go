//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

const rv64PublicGCRefCastAbstractHex = "0061736d01000000019180808000056000005f005e780060016f0060017f000388808080000700030404040404048480808000016e000a07de808080000604696e69740001117265665f636173745f6e6f6e5f6e756c6c00020d7265665f636173745f6e756c6c00030c7265665f636173745f69333100040f7265665f636173745f73747275637400050e7265665f636173745f6172726179000609858080800001030001000ad081808000078280808000000bba80808000004100d06e260041014107fb1c26004102fb0101260041034100fb0702260041042000fb1a26004105d06c26004106d06b26004107d07126000b90808080000020002500d41a20002500fb176e1a0baa808080000020002500fb176e1a20002500fb176b1a20002500fb176a1a20002500fb176c1a20002500fb17711a0b92808080000020002500fb166c1a20002500fb176c1a0b92808080000020002500fb166b1a20002500fb176b1a0b92808080000020002500fb166a1a20002500fb176a1a0b"

func rv64PublicGCRefCastAbstractBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCRefCastAbstractHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCRefCastAbstractCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCRefCastAbstractBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func TestRV64PublicGCRefCastAbstractExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCRefCastAbstractBytes(t)
	if len(data) != 380 || sha256.Sum256(data) != [sha256.Size]byte{0xc8, 0x55, 0x56, 0x08, 0x9b, 0xf1, 0xa3, 0x9c, 0xb3, 0x08, 0x2f, 0x7d, 0xe9, 0x16, 0xc0, 0x0e, 0xaa, 0x24, 0x82, 0x25, 0x3c, 0xf1, 0x26, 0xd8, 0xa9, 0xfc, 0x09, 0xab, 0x97, 0x0e, 0xed, 0x4b} {
		t.Fatal("abstract gc/ref_cast source identity changed")
	}
	cases := []struct {
		name  string
		wants []TrapCode
	}{
		{"ref_cast_non_null", []TrapCode{TrapNullReference, 0, 0, 0, 0, TrapNullReference, TrapNullReference, TrapNullReference}},
		{"ref_cast_null", []TrapCode{0, TrapCastFailure, TrapCastFailure, TrapCastFailure, TrapCastFailure, 0, 0, 0}},
		{"ref_cast_i31", []TrapCode{TrapCastFailure, 0, TrapCastFailure, TrapCastFailure, TrapCastFailure, TrapCastFailure, TrapCastFailure, TrapCastFailure}},
		{"ref_cast_struct", []TrapCode{TrapCastFailure, TrapCastFailure, 0, TrapCastFailure, TrapCastFailure, TrapCastFailure, TrapCastFailure, TrapCastFailure}},
		{"ref_cast_array", []TrapCode{TrapCastFailure, TrapCastFailure, TrapCastFailure, 0, TrapCastFailure, TrapCastFailure, TrapCastFailure, TrapCastFailure}},
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCRefCastAbstractCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || railshotValidateCompiled(view) != nil {
				t.Fatalf("missing exact abstract gc/ref_cast witness: exact=%t validation=%v product=%v code=%d/%x", rv64GCRefCastAbstract(view), railshotValidateCompiled(view), view.stagedGCStructProduct(), len(view.code), sha256.Sum256(view.code))
			}
			if params, results, err := view.maxCallSlots(); err != nil || params != 4 || results != 1 {
				t.Fatalf("abstract gc/ref_cast call capacity=%d/%d,%v, want 4/1", params, results, err)
			}
			for _, profile := range []struct {
				name string
				gc   GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
					defer rt.Close()
					mod, err := rt.Module(compiled)
					if err != nil {
						t.Fatal(err)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(profile.gc))
					mod.Close()
					if err != nil {
						t.Fatal(err)
					}
					state := in.existingGCRefTestTableState()
					if in.gc == nil || state == nil || state.TableCount != 1 || state.Count != 10 || len(state.Descriptors) != 1 || state.Conversion == nil || in.refStore == nil {
						t.Fatalf("collector/ref.cast table state=%v/%+v store=%p", in.gc, state, in.refStore)
					}
					foreign, err := in.NewExternRef("cast-foreign")
					if err != nil {
						t.Fatal(err)
					}
					for iteration := 0; iteration < 5; iteration++ {
						if got, err := in.Invoke("init", ValueExternRef(foreign).Bits()); err != nil || len(got) != 0 {
							t.Fatalf("init iteration %d=%v,%v", iteration, got, err)
						}
					}
					for _, index := range []int{2, 3} {
						word := binary.LittleEndian.Uint64(state.Descriptor[8+index*8:])
						root, err := in.gc.CheckedTableSlot(state.Slots[index])
						cast, castErr := state.refCast(in.gc, word, corergc.RefTestTarget{Kind: corergc.RefTestAny, Nullable: true})
						if err != nil || word != uint64(root) || castErr != nil || cast != word {
							t.Fatalf("rooted object %d word=%#x root=%#x err=%v cast=%#x,%v", index, word, root, err, cast, castErr)
						}
					}
					if got, err := in.Invoke("ref_cast_non_null", uint64(0xfeedface)<<32|2); err != nil || len(got) != 0 {
						t.Fatalf("isolated ref_cast_non_null object=%v,%v", got, err)
					}
					runCases := func(phase string) {
						t.Helper()
						for _, tc := range cases {
							for index, want := range tc.wants {
								dirty := uint64(0xfeedface)<<32 | uint64(index)
								got, err := in.Invoke(tc.name, dirty)
								if want == 0 {
									if err != nil || len(got) != 0 {
										t.Fatalf("%s %s(%#x)=%v,%v", phase, tc.name, dirty, got, err)
									}
									continue
								}
								var trap *TrapError
								if got != nil || !errors.As(err, &trap) || trap.Code != want {
									t.Fatalf("%s %s(%#x)=%v,%v want trap %v", phase, tc.name, dirty, got, err, want)
								}
							}
						}
					}
					runCases("before-gc")
					for _, name := range []string{"ref_cast_non_null", "ref_cast_null", "ref_cast_i31", "ref_cast_struct", "ref_cast_array"} {
						got, err := in.Invoke(name, uint64(0xfeedface)<<32|10)
						var trap *TrapError
						if got != nil || !errors.As(err, &trap) || trap.Code != TrapTableOutOfBounds {
							t.Fatalf("%s dirty out-of-bounds=%v,%v", name, got, err)
						}
					}
					if state.Conversion.count != 1 {
						t.Fatalf("conversion entries=%d want 1", state.Conversion.count)
					}
					if err := in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 2 {
						t.Fatalf("live objects=%d want struct+array", live)
					}
					runCases("after-gc")
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if !state.Conversion.closed || state.Conversion.count != 0 {
						t.Fatalf("closed conversion state=%t/%d", state.Conversion.closed, state.Conversion.count)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCRefCastAbstractAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_REF_CAST_ABSTRACT_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCRefCastAbstractCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[6]++; v.InternalEntry[6]++ },
		"function":   func(v *Compiled) { v.Funcs[5].Results = []ValType{ValI32} },
		"type":       func(v *Compiled) { v.Types[2].Array.Mutable = true },
		"type-key":   func(v *Compiled) { v.FuncTypeID[3] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[2].ElemSize = 2 },
		"value-type": func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Abstract = AbstractHeapEq },
		"table":      func(v *Compiled) { v.TableSize++ },
		"export":     func(v *Compiled) { delete(v.Exports, "ref_cast_array"); v.Exports["other"] = 6 },
		"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if rv64SourceContractValid(candidate) {
				t.Fatal("hostile retained mutation kept abstract gc/ref_cast witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained abstract gc/ref_cast mutation admitted", err)
			}
		})
	}
}

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
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64PublicGCRefTestAbstractHex = "0061736d01000000019280808000056000005f005e780060016f0060017f017f038d808080000c000304040404040404040404048a80808000036e000a70000a6f000a07ba818080000b04696e69740001127265665f746573745f6e756c6c5f6461746100020c7265665f746573745f616e7900030b7265665f746573745f657100040c7265665f746573745f69333100050f7265665f746573745f73747275637400060e7265665f746573745f61727261790007127265665f746573745f6e756c6c5f66756e6300080d7265665f746573745f66756e630009147265665f746573745f6e756c6c5f65787465726e000a0f7265665f746573745f65787465726e000b09858080800001030001000ade828080000c8280808000000bfb80808000004100d06e26004101d06b26004102d071260041034107fb1c26004104fb0101260041054100fb0702260041062000fb1a26004107d06ffb1a26004100d07326014101d07026014102d20026014100d07226024101d06f260241022000260241034108fb1cfb1b26024104fb0101fb1b26024105d06efb1b26020b8f808080000020002500d120002500fb15716a0b91808080000020002500fb146e20002500fb156e6a0b91808080000020002500fb146d20002500fb156d6a0b91808080000020002500fb146c20002500fb156c6a0b91808080000020002500fb146b20002500fb156b6a0b91808080000020002500fb146a20002500fb156a6a0b8f808080000020002501d120002501fb15736a0b91808080000020002501fb147020002501fb15706a0b8f808080000020002502d120002502fb15726a0b91808080000020002502fb146f20002502fb156f6a0b"

func rv64PublicGCRefTestAbstractBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCRefTestAbstractHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func rv64GCRefTestAbstractCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCRefTestAbstractBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func TestRV64PublicGCRefTestAbstractExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCRefTestAbstractBytes(t)
	if len(data) != 626 || sha256.Sum256(data) != [sha256.Size]byte{0x52, 0x6d, 0x5c, 0x1b, 0x45, 0x7f, 0x84, 0x7d, 0xaf, 0x51, 0x14, 0x1a, 0x7d, 0x63, 0xab, 0xa1, 0x1d, 0x20, 0x41, 0x5b, 0x7e, 0xf2, 0xa1, 0x3f, 0x59, 0x3e, 0x06, 0xf6, 0x80, 0xa4, 0x14, 0x03} {
		t.Fatal("gc abstract ref.test source identity changed")
	}
	cases := map[string][]uint64{
		"ref_test_null_data":   {2, 2, 2, 0, 0, 0, 0, 2},
		"ref_test_any":         {1, 1, 1, 2, 2, 2, 2, 1},
		"ref_test_eq":          {1, 1, 1, 2, 2, 2, 0, 1},
		"ref_test_i31":         {1, 1, 1, 2, 0, 0, 0, 1},
		"ref_test_struct":      {1, 1, 1, 0, 2, 0, 0, 1},
		"ref_test_array":       {1, 1, 1, 0, 0, 2, 0, 1},
		"ref_test_null_func":   {2, 2, 0},
		"ref_test_func":        {1, 1, 2},
		"ref_test_null_extern": {2, 2, 0, 0, 0, 2},
		"ref_test_extern":      {1, 1, 2, 2, 2, 1},
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCRefTestAbstractCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || railshotValidateCompiled(view) != nil {
				t.Fatalf("missing exact abstract ref.test witness: exact=%t validation=%v product=%v code=%d/%x entries=%v/%v", rv64GCRefTestAbstract(view), railshotValidateCompiled(view), view.stagedGCStructProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			if params, results, err := view.maxCallSlots(); err != nil || params != 4 || results != 1 {
				t.Fatalf("abstract ref.test call capacity = %d/%d, %v; want 4/1", params, results, err)
			}
			for _, profile := range []struct {
				name string
				gc   GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true, StressBarriers: true}}} {
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
					defer in.Close()
					state := in.existingGCRefTestTableState()
					if in.gc == nil || state == nil || state.TableCount != 3 || state.Count != 10 || len(state.Descriptors) != 3 || state.Conversion == nil || in.refStore == nil {
						t.Fatalf("collector/mixed table state=%v/%+v store=%p", in.gc, state, in.refStore)
					}
					extern, err := in.NewExternRef("official-extern")
					if err != nil {
						t.Fatal(err)
					}
					token := ValueExternRef(extern).Bits()
					for iteration := 0; iteration < 5; iteration++ {
						if got, err := in.Invoke("init", token); err != nil || len(got) != 0 {
							t.Fatalf("init iteration %d=%v,%v", iteration, got, err)
						}
						for name, expected := range cases {
							for index, want := range expected {
								got, err := in.Invoke(name, uint64(index))
								if err != nil || !slices.Equal(got, []uint64{want}) {
									t.Fatalf("%s(%d) iteration %d=%v,%v want [%d]", name, index, iteration, got, err, want)
								}
							}
						}
					}
					any := state.Descriptors[0]
					if got := binary.LittleEndian.Uint64(any[8+3*8:]); got != uint64(gc.I31New(7)) {
						t.Fatalf("any table i31=%#x want %#x", got, gc.I31New(7))
					}
					for _, index := range []int{4, 5} {
						word := binary.LittleEndian.Uint64(any[8+index*8:])
						root, err := in.gc.CheckedTableSlot(state.Slots[index])
						if err != nil || word != uint64(root) || !root.IsObj() {
							t.Fatalf("any table object %d=%#x root=%#x err=%v", index, word, root, err)
						}
					}
					if descriptor := binary.LittleEndian.Uint64(state.Descriptors[1][8+2*32+24:]); descriptor == 0 {
						t.Fatal("funcref table did not retain local function identity")
					}
					if got := binary.LittleEndian.Uint64(state.Descriptors[2][8+2*8:]); got != token {
						t.Fatalf("extern table public token=%#x want %#x", got, token)
					}
					if state.Conversion.count != 3 {
						t.Fatalf("conversion entries=%d want 3", state.Conversion.count)
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					for name, expected := range cases {
						for index, want := range expected {
							got, err := in.Invoke(name, uint64(index))
							if err != nil || !slices.Equal(got, []uint64{want}) {
								t.Fatalf("%s(%d) after GC=%v,%v want [%d]", name, index, got, err, want)
							}
						}
					}
				})
			}
		})
	}
}

func TestRV64PublicGCRefTestAbstractAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_REF_TEST_ABSTRACT_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64GCRefTestAbstractCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":          func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":         func(v *Compiled) { v.Entry[11]++; v.InternalEntry[11]++ },
		"function":      func(v *Compiled) { v.Funcs[2].Results = nil },
		"type":          func(v *Compiled) { v.Types[2].Array.Mutable = true },
		"type-key":      func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor":    func(v *Compiled) { v.GCTypeDescs[2].ElemSize = 2 },
		"table-type":    func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Abstract = AbstractHeapEq },
		"extra-table":   func(v *Compiled) { v.extraTables[1].Type = ValFuncRef },
		"table-size":    func(v *Compiled) { v.TableSize++ },
		"export":        func(v *Compiled) { delete(v.Exports, "ref_test_any"); v.Exports["other"] = 3 },
		"missing-gc":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"extra-feature": func(v *Compiled) { v.requiredFeatures |= CoreFeatureSIMD },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if rv64SourceContractValid(candidate) {
				t.Fatal("hostile retained mutation kept the abstract ref.test witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained abstract ref.test mutation admitted", err)
			}
		})
	}
}

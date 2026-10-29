//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

const rv64PublicGCRefTestConcreteHex = "0061736d0100000001bf808080000950005f005001005f017f005001005f017f005001015f027f007f005001025f027f007f005001005f027f007f005001005f005001065f027f007f0060000003848080800003080808048480808000016b00140799808080000208746573742d73756200010a746573742d63616e6f6e00020aca8680800003ba80808000004100fb01002600410afb010026004101fb01012600410bfb010226004102fb01032600410cfb010426004103fb010526004104fb010726000beb848080000010000240d06bfb1500450d00d000fb1500450d00d001fb1500450d00d003fb1500450d00d005fb1500450d00d007fb1500450d0041002500fb1500450d0041012500fb1500450d0041022500fb1500450d0041032500fb1500450d0041042500fb1500450d00d06bfb1501450d00d000fb1501450d00d001fb1501450d00d003fb1501450d00d005fb1501450d00d007fb1501450d0041012500fb1501450d0041022500fb1501450d00d06bfb1503450d00d000fb1503450d00d001fb1503450d00d003fb1503450d00d005fb1503450d00d007fb1503450d0041022500fb1503450d00d06bfb1505450d00d000fb1505450d00d001fb1505450d00d003fb1505450d00d005fb1505450d00d007fb1505450d0041032500fb1505450d00d06bfb1507450d00d000fb1507450d00d001fb1507450d00d003fb1507450d00d005fb1507450d00d007fb1507450d0041042500fb1507450d0041002500fb1400450d0041012500fb1400450d0041022500fb1400450d0041032500fb1400450d0041042500fb1400450d0041012500fb1401450d0041022500fb1401450d0041022500fb1403450d0041032500fb1405450d0041042500fb1407450d00d06bfb14000d00d06bfb14010d00d06bfb14030d00d06bfb14050d00d06bfb14070d0041002500fb14010d0041032500fb14010d0041042500fb14010d0041002500fb14030d0041012500fb14030d0041032500fb14030d0041042500fb14030d0041002500fb14050d0041012500fb14050d0041022500fb14050d0041042500fb14050d0041002500fb14070d0041012500fb14070d0041022500fb14070d0041032500fb14070d000f0b000b9581808000001000024041002500fb1400450d0041012500fb1400450d0041022500fb1400450d0041032500fb1400450d0041042500fb1400450d00410a2500fb1400450d00410b2500fb1400450d00410c2500fb1400450d0041012500fb1402450d0041022500fb1402450d00410b2500fb1401450d00410c2500fb1401450d0041022500fb1404450d00410c2500fb1403450d000f0b000b"

func rv64PublicGCRefTestConcreteBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCRefTestConcreteHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func rv64GCRefTestConcreteCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCRefTestConcreteBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func TestRV64PublicGCRefTestConcreteExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCRefTestConcreteBytes(t)
	if len(data) != 976 || sha256.Sum256(data) != [sha256.Size]byte{0x7a, 0x71, 0xf9, 0x66, 0x22, 0x07, 0x79, 0x9b, 0x26, 0x2c, 0xcb, 0xc7, 0x90, 0x9f, 0x4e, 0x94, 0x92, 0xc0, 0x4f, 0x71, 0x73, 0xf8, 0x4f, 0x29, 0xbe, 0x69, 0x90, 0x5d, 0x92, 0x5f, 0x64, 0x26} {
		t.Fatal("gc concrete ref.test source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCRefTestConcreteCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || railshotValidateCompiled(view) != nil {
				t.Fatalf("missing exact concrete ref.test witness: exact=%t validation=%v product=%v staged=%v required=%v code=%d/%x entries=%v/%v flags=ctx:%t tablectx:%t tableref:%t regoff:%t table64:%v hdr:%t exports:%t desc:%t escape:%t ref-fn:%t ref-call:%t ref-control:%t tail:%t funcs=%+v ids=%v types=%+v descs=%+v value=%+v", rv64GCRefTestConcrete(view), railshotValidateCompiled(view), view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.nativeContextTrapABI, view.nativeTableContextABI, view.nativeTableReferenceABI, view.registerABIDisabled, view.nativeTable64ABI, view.needsFuncRefContextHeader, view.hasTableExportMetadata, view.NeedsFuncRefDescs, view.dynamicFuncrefEscape, view.nativeReferenceFunctionABI, view.nativeReferenceCallABI, view.nativeReferenceControlABI, rv64HasReferenceTail(view), view.Funcs, view.FuncTypeID, view.Types, view.GCTypeDescs, view.ValueTypes)
			}
			if params, results, err := view.maxCallSlots(); err != nil || params != 0 || results != 0 {
				t.Fatalf("concrete ref.test call capacity = %d/%d, %v; want 0/0", params, results, err)
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
					if in.gc == nil {
						t.Fatal("table execution has no collector")
					}
					for iteration := 0; iteration < 5; iteration++ {
						for _, name := range []string{"test-sub", "test-canon"} {
							if got, err := in.Invoke(name); err != nil || len(got) != 0 {
								t.Fatalf("%s iteration %d=%v,%v", name, iteration, got, err)
							}
						}
					}
					roots := rv64PublicGCTableRoots(t, in, 20)
					for slot, typeIndex := range map[int]int{0: 0, 10: 0, 1: 1, 11: 2, 2: 3, 12: 4, 3: 5, 4: 7} {
						rooted := roots[slot].GetRef()
						want, ok := in.gcDomainType(uint32(typeIndex))
						if !ok {
							t.Fatalf("slot %d module type %d has no Runtime-domain identity", slot, typeIndex)
						}
						actual, err := in.gc.ObjectType(rooted)
						if err != nil || actual != want {
							t.Fatalf("slot %d type=%d,%v want module type %d (Runtime type %d)", slot, actual, err, typeIndex, want)
						}
					}
					if rooted := roots[19].GetRef(); !rooted.IsNull() {
						t.Fatalf("unused slot root=%#x want null", rooted)
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					for _, name := range []string{"test-sub", "test-canon"} {
						if got, err := in.Invoke(name); err != nil || len(got) != 0 {
							t.Fatalf("%s after GC=%v,%v", name, got, err)
						}
					}
				})
			}
		})
	}
}

func TestRV64PublicGCRefTestConcreteAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_REF_TEST_CONCRETE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64GCRefTestConcreteCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":          func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":         func(v *Compiled) { v.Entry[2]++; v.InternalEntry[2]++ },
		"function":      func(v *Compiled) { v.Funcs[1].Results = []ValType{ValI32} },
		"type-super":    func(v *Compiled) { v.Types[1].Supers[0] = 6 },
		"type-key":      func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor":    func(v *Compiled) { v.GCTypeDescs[3].Super = 2 },
		"table-type":    func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Abstract = AbstractHeapEq },
		"table-size":    func(v *Compiled) { v.TableSize++ },
		"table-init":    func(v *Compiled) { v.HasTableInitFunc = true },
		"export":        func(v *Compiled) { delete(v.Exports, "test-sub"); v.Exports["other"] = 1 },
		"missing-gc":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"extra-feature": func(v *Compiled) { v.requiredFeatures |= CoreFeatureSIMD },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if rv64SourceContractValid(candidate) {
				t.Fatal("hostile retained mutation kept the concrete ref.test witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained concrete ref.test mutation admitted", err)
			}
		})
	}
}

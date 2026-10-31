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
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64PublicGCRefTestTableHex = "0061736d01000000019b808080000550005f005001005f017f005001005f017e0060000060017f017f0385808080000403040404048480808000016b0002079e808080000404696e6974000004626173650001046c656674000205726967687400030ac080808000049080808000004100fb010126004101fb010226000b89808080000020002500fb14000b89808080000020002500fb14010b89808080000020002500fb14020b"

func rv64PublicGCRefTestTableBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(rv64PublicGCRefTestTableHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func rv64GCRefTestTableCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	original, err := rv64I31Config().Compile(rv64PublicGCRefTestTableBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })

	return original

}

func TestRV64PublicGCRefTestTableExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCRefTestTableBytes(t)
	if len(data) != 168 || sha256.Sum256(data) != [sha256.Size]byte{0xab, 0x93, 0xf4, 0x6c, 0x27, 0x1d, 0x3e, 0x1a, 0x71, 0xc2, 0x1d, 0xa7, 0x25, 0x7e, 0x29, 0xb2, 0x36, 0x3e, 0x91, 0x88, 0x72, 0x53, 0x78, 0x00, 0x57, 0x05, 0xb3, 0x3a, 0x05, 0x6a, 0x8c, 0xbd} {
		t.Fatal("gc object-table ref.test source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCRefTestTableCompile(t, artifact)
			view := compiled.executionView()
			if !rv64SourceContractValid(view) || railshotValidateCompiled(view) != nil {
				t.Fatalf("missing exact object-table ref.test witness: exact=%t validation=%v product=%v staged=%v required=%v code=%d/%x entries=%v/%v table=%v/%v/%d/%d/%v valueTypes=%+v types=%+v descs=%+v", rv64GCRefTestTable(view), railshotValidateCompiled(view), view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.TableType, view.TableHasValueType, view.TableSize, view.TableMax, view.TableHasMax, view.ValueTypes, view.Types, view.GCTypeDescs)
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
						if _, err := in.Invoke("init"); err != nil {
							t.Fatal(err)
						}
					}
					for _, tc := range []struct {
						name string
						slot uint64
						want uint64
					}{{"base", 0xdeadbeef00000000, 1}, {"base", 0xffffffff00000001, 1}, {"left", 0, 1}, {"left", 1, 0}, {"right", 0, 0}, {"right", 1, 1}} {
						got, err := in.Invoke(tc.name, tc.slot)
						if err != nil || !slices.Equal(got, []uint64{tc.want}) {
							t.Fatalf("%s(%#x)=%v,%v want %d", tc.name, tc.slot, got, err, tc.want)
						}
					}
					if got, err := in.Invoke("base", 2); got != nil || err == nil {
						t.Fatalf("base(2)=%v,%v want bounds trap", got, err)
					} else {
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds {
							t.Fatalf("base(2) trap=%v", err)
						}
					}
					var roots []gc.Ref
					in.rangeLocalGCTableRoots(func(slot gc.RootSlot) bool {
						roots = append(roots, slot.GetRef())
						return true
					}, nil)
					if len(roots) != 2 {
						t.Fatalf("table root count=%d, want two", len(roots))
					}
					for i, want := range []gc.TypeID{1, 2} {
						actual, err := in.gc.ObjectType(roots[i])
						if err != nil || actual != want {
							t.Fatalf("slot %d type=%d,%v want %d", i, actual, err, want)
						}
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if got, err := in.Invoke("left", 0); err != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatalf("left(0) after GC=%v,%v", got, err)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCRefTestTableAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_REF_TEST_TABLE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64GCRefTestTableCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":                func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":               func(v *Compiled) { v.Entry[3]++; v.InternalEntry[3]++ },
		"function":            func(v *Compiled) { v.Funcs[1].Params[0] = ValI64 },
		"type-super":          func(v *Compiled) { v.Types[1].Supers[0] = 2 },
		"type-key":            func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor":          func(v *Compiled) { v.GCTypeDescs[1].Super = 2 },
		"table-type":          func(v *Compiled) { v.ValueTypes[0].Ref.Heap.Abstract = AbstractHeapEq },
		"table-size":          func(v *Compiled) { v.TableSize++ },
		"table-init":          func(v *Compiled) { v.HasTableInitFunc = true },
		"inactive-table-init": func(v *Compiled) { v.TableInitFunc = 1 },
		"export":              func(v *Compiled) { delete(v.Exports, "right"); v.Exports["other"] = 3 },
		"zero-export":         func(v *Compiled) { delete(v.Exports, "init"); v.Exports["other"] = 0 },
		"missing-gc":          func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"extra-feature":       func(v *Compiled) { v.requiredFeatures |= CoreFeatureSIMD },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if rv64SourceContractValid(candidate) {
				t.Fatal("hostile retained mutation kept the object-table ref.test witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained object-table ref.test mutation admitted", err)
			}
		})
	}
}

//go:build amd64

package dragline

import (
	"bytes"
	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestAMD64SIMDUsesAllocatedMemoryAddress(t *testing.T) {
	for _, store := range []bool{false, true} {
		for _, signals := range []bool{false, true} {
			params, results := []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}
			body := []byte{0x20, 0, 0xfd, 0, 0, 0, 0x0b}
			if store {
				params = append(params, wasm.V128)
				results = nil
				body = []byte{0x20, 0, 0x20, 1, 0xfd, 0x0b, 0, 0, 0x0b}
			}
			fn, plan := simdAddressTestPlan(t, params, results, body)
			plan.SignalsBounds = signals
			native, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
			if err != nil || !used {
				t.Fatalf("emit: %t %v", used, err)
			}
			found := false
			for id, in := range plan.Machine.Insts {
				if in.Op != railmach.OpAMD64V128Load && in.Op != railmach.OpAMD64V128Store {
					continue
				}
				operands := plan.Machine.InstructionOperands(uint32(id))
				pos := plan.Allocation.InstructionPositions[id]*6 + 2
				address := plan.Allocation.LocationAt(operands[0].Reg, pos)
				if address.Kind != railmach.LocationRegister {
					t.Fatal("fixture address is not register allocated")
				}
				var want amd64.Asm
				if store {
					value := plan.Allocation.LocationAt(operands[1].Reg, pos)
					want.VMovdquStoreIdx(amd64.RBX, amd64RailMachGPRRegisters[address.Index], amd64FPRRegisters[value.Index], 0)
				} else {
					value := plan.Allocation.LocationAt(in.Result, pos+1)
					want.VMovdquLoadIdx(amd64FPRRegisters[value.Index], amd64.RBX, amd64RailMachGPRRegisters[address.Index], 0)
				}
				if !bytes.Contains(native, want.B) {
					t.Fatalf("store=%t signals=%t: missing direct memory encoding %x in %x", store, signals, want.B, native)
				}
				found = true
			}
			if !found {
				t.Fatal("fixture did not select vector memory")
			}
		}
	}
}

func TestAMD64F64SplatUsesThreeOperandUnpack(t *testing.T) {
	fn, plan := simdAddressTestPlan(t, []wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64, wasm.V128}, []byte{0x20, 0, 0x20, 0, 0xfd, 0x14, 0x0b})
	native, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !used {
		t.Fatalf("emit: %t %v", used, err)
	}
	found := false
	for id, in := range plan.Machine.Insts {
		if in.Op != railmach.OpAMD64F64x2Splat {
			continue
		}
		pos := plan.Allocation.InstructionPositions[id]*6 + 2
		src := plan.Allocation.LocationAt(plan.Machine.InstructionOperands(uint32(id))[0].Reg, pos)
		dst := plan.Allocation.LocationAt(in.Result, pos+1)
		if src.Kind != railmach.LocationRegister || dst.Kind != railmach.LocationRegister {
			t.Fatal("fixture splat is not register allocated")
		}
		var want amd64.Asm
		want.VSseRRR(0b01, 0x6c, amd64FPRRegisters[dst.Index], amd64FPRRegisters[src.Index], amd64FPRRegisters[src.Index])
		if !bytes.Contains(native, want.B) {
			t.Fatalf("missing single-instruction splat %x in %x", want.B, native)
		}
		found = true
	}
	if !found {
		t.Fatal("fixture did not select f64 splat")
	}
}

func simdAddressTestPlan(t *testing.T, params, results []wasm.ValType, body []byte) (*railssa.Func, *nativeBackendPlan) {
	t.Helper()
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&nativeBackendPlanner{}).Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	return fn, plan
}

package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

type scalarParameterTarget struct {
	scalarTestTarget
	normalizations        int
	stores, loads         int
	destination, returned uint8
}

func (*scalarParameterTarget) ParameterRegister(int) (uint8, bool) { return 3, true }
func (t *scalarParameterTarget) NormalizeI32(uint8)                { t.normalizations++ }
func (t *scalarParameterTarget) Store(int32, uint8, bool)          { t.stores++ }
func (t *scalarParameterTarget) Load(uint8, int32, bool)           { t.loads++ }
func (t *scalarParameterTarget) Binary(_ IntOp, _ bool, d, _ uint8, _ ScalarOperand) {
	t.destination = d
}
func (t *scalarParameterTarget) Return(r uint8, _ bool, _ int) { t.returned = r }

func TestScalarIncomingRegisterAndReturnOwnership(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, add := wasm.I32, byte(0x41), byte(0x6a)
		if wide {
			typ, constant, add = wasm.I64, 0x42, 0x7c
		}
		for _, tail := range []bool{false, true} {
			body := []byte{0x20, 0, constant, 1, add}
			if tail {
				body = append(body, 0x0f)
			}
			body = append(body, 0x0b)
			ft := &wasm.CompType{Params: []wasm.ValType{typ}, Results: []wasm.ValType{typ}}
			summary := AdmitScalar(body, ft, []wasm.ValType{typ})
			target := &scalarParameterTarget{}
			var state ScalarState
			if _, err := state.CompileScalar(body, summary, []bool{wide}, 1, target); err != nil {
				t.Fatal(err)
			}
			if target.normalizations != 0 || target.stores != 0 || target.loads != 0 || target.destination != 3 || target.returned != 3 {
				t.Fatalf("wide=%v tail=%v: entry/return round trip: %+v", wide, tail, target)
			}
			scalarAssertFreeList(t, &state)
			for _, id := range state.owners {
				if id != 0 {
					t.Fatal("return retained register owner")
				}
			}
		}
	}
}

func TestScalarIncomingRegisterHasNoImplicitHome(t *testing.T) {
	target := &scalarParameterTarget{}
	var state ScalarState
	state.target = target
	state.add(scalarNode{})
	id := state.add(scalarNode{kind: ScalarRegister, reg: 3, refs: 2, wide: true})
	state.owners[3] = id
	state.spill(id)
	if target.stores != 1 || state.Spills != 1 || state.node(id).kind != ScalarFrame || state.node(id).home != 0 {
		t.Fatal("incoming register was evicted to an uninitialized local home")
	}
	state.regs = scalarTestRegs
	state.materialize(id, 0)
	if target.loads != 1 || state.Reloads != 1 {
		t.Fatal("spilled incoming register was not reloaded")
	}
	state.release(id)
	state.release(id)
	scalarAssertFreeList(t, &state)
}

func TestScalarRawIncomingReturnNormalization(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ := wasm.I32
		if wide {
			typ = wasm.I64
		}
		body := []byte{0x20, 0, 0x22, 0, 0x0f, 0x0b}
		ft := &wasm.CompType{Params: []wasm.ValType{typ}, Results: []wasm.ValType{typ}}
		target := &scalarParameterTarget{}
		var state ScalarState
		if _, err := state.CompileScalar(body, AdmitScalar(body, ft, []wasm.ValType{typ}), []bool{wide}, 1, target); err != nil {
			t.Fatal(err)
		}
		want := 1
		if wide {
			want = 0
		}
		if target.normalizations != want || target.returned != 3 {
			t.Fatalf("wide=%v target=%+v", wide, target)
		}
		scalarAssertFreeList(t, &state)
	}
}

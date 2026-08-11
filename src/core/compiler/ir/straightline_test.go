package ir

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestBuildStraightLinePlanRenamesLocalVersions(t *testing.T) {
	// local.get 0; local.get 1; i32.add; local.tee 1; local.get 1; i32.xor
	body := []byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x22, 0x01, 0x20, 0x01, 0x73, 0x0b}
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}},
		[]uint32{0}, nil, nil, nil, [][]byte{wasmtest.Code(body)}))
	f, err := BuildFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := BuildStraightLinePlan(f)
	if p == nil || len(p.InitialLocals) != 2 {
		t.Fatalf("plan = %#v", p)
	}
	tee := f.Insts[3]
	get := f.Insts[4]
	teeValue := p.Resolve(f.ValueIDs[tee.Results.Start])
	getValue := p.Resolve(f.ValueIDs[get.Results.Start])
	if teeValue != getValue {
		t.Fatalf("post-tee get resolves to %d, want tee version %d", getValue, teeValue)
	}
	if p.Resolve(p.InitialLocals[1]) == teeValue {
		t.Fatal("local assignment rewrote the initial local identity")
	}
}

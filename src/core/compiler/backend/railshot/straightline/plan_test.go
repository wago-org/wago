package straightline

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestBuildFuncPlansValidatedBytesAndRenamesLocals(t *testing.T) {
	// local 1 = local 0 + local 1; memory[local 1] = 0
	body := []byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x21, 0x01, 0x20, 0x01, 0x41, 0x00, 0x36, 0x02, 0x00, 0x0b}
	m := testModule(t, body)
	f, err := BuildFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := BuildStraightLinePlan(f)
	if p == nil || len(p.InitialLocals) != 2 {
		t.Fatalf("plan = %#v", p)
	}
	set := f.Insts[3]
	get := f.Insts[4]
	written := p.Resolve(f.ValueIDs[set.Args.Start])
	read := p.Resolve(f.ValueIDs[get.Results.Start])
	if read != written {
		t.Fatalf("post-set get resolves to %d, want %d", read, written)
	}
	if p.Resolve(p.InitialLocals[1]) == written {
		t.Fatal("assignment rewrote the initial local identity")
	}
}

func TestBuildFuncRejectsUnsupportedShape(t *testing.T) {
	// memory.size; drop is valid Wasm but intentionally outside this planner.
	if _, err := BuildFunc(testModule(t, []byte{0x3f, 0x00, 0x1a, 0x0b}), 0); err == nil {
		t.Fatal("BuildFunc accepted unsupported memory.size")
	}
}

func testModule(t *testing.T, body []byte) *wasm.Module {
	t.Helper()
	i32s := []wasm.ValType{wasm.I32, wasm.I32}
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32s, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

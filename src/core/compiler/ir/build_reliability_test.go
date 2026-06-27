package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestBuildIsDeterministicAcrossRuns(t *testing.T) {
	body := wasmtest.Code(bytes(
		0x20, 0x00,
		0x04, byte(wasm.I32),
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x41, 0x03,
		0x6a,
		0x0b,
	))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	var first string
	for i := 0; i < 10; i++ {
		f, err := BuildFunc(m, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyFunc(f); err != nil {
			t.Fatal(err)
		}
		got := FormatFunc(f)
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatalf("build %d produced different dump\nfirst:\n%s\ngot:\n%s", i, first, got)
		}
	}
}

func TestBuildEntryParamsAndLocalDecls(t *testing.T) {
	body := codeWithLocals([]wasm.LocalEntry{{Count: 2, Type: wasm.I64}, {Count: 1, Type: wasm.F32}}, bytes(
		0x20, 0x00,
		0x20, 0x01,
		0x20, 0x02,
		0x1a,
		0x1a,
		0x0b,
	))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32, wasm.I64}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	f, dump := buildOne(t, m)
	if len(f.Locals) != 5 {
		t.Fatalf("locals len = %d, want 5", len(f.Locals))
	}
	wantLocals := []wasm.ValType{wasm.I32, wasm.I64, wasm.I64, wasm.I64, wasm.F32}
	for i, want := range wantLocals {
		if f.Locals[i] != want {
			t.Fatalf("local %d type = %s, want %s", i, f.Locals[i], want)
		}
	}
	entryParams := f.Blocks[f.Entry].Params
	if entryParams.Len != 2 {
		t.Fatalf("entry params = %d, want 2", entryParams.Len)
	}
	for i := uint32(0); i < entryParams.Len; i++ {
		v := f.ValueIDs[entryParams.Start+i]
		if f.Values[v].DefKind != ValueDefBlockParam || f.Values[v].Def != uint32(f.Entry) {
			t.Fatalf("entry param %d bad def: %+v", i, f.Values[v])
		}
	}
	for _, want := range []string{"local.get 0", "local.get 1", "local.get 2"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("dump missing %q:\n%s", want, dump)
		}
	}
}

func TestBuildNestedLabelDepths(t *testing.T) {
	// Branch from the inner block to the outer block's merge with a value.
	body := wasmtest.Code(bytes(
		0x02, byte(wasm.I32),
		0x02, byte(wasm.I32),
		0x41, 0x63,
		0x0c, 0x01,
		0x0b,
		0x0b,
		0x0b,
	))
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	f, dump := buildOne(t, m)
	if !strings.Contains(dump, "br b2") {
		t.Fatalf("expected branch to outer merge b2:\n%s", dump)
	}
	if len(f.Edges) == 0 || f.Edges[0].To != 1 {
		// Initial entry branch goes to outer body b1.
		t.Fatalf("unexpected first edge set: %+v", f.Edges)
	}
}

func TestBuildBrTableOnlyDefault(t *testing.T) {
	body := wasmtest.Code(bytes(
		0x02, byte(wasm.I32),
		0x41, 0x05,
		0x20, 0x00,
		0x0e, 0x00, 0x00,
		0x0b,
		0x0b,
	))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	_, dump := buildOne(t, m)
	if !strings.Contains(dump, "switch %") || !strings.Contains(dump, "default:b") || strings.Contains(dump, " 0:b") {
		t.Fatalf("unexpected br_table default-only dump:\n%s", dump)
	}
}

func TestBuildBrTableMultipleTargetsPreservesArgumentTypes(t *testing.T) {
	types := []wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32, wasm.I64}}, {Results: []wasm.ValType{wasm.I32, wasm.I64}}}
	body := wasmtest.Code(bytes(
		0x02, 0x01,
		0x02, 0x01,
		0x41, 0x07,
		0x42, 0x08,
		0x20, 0x00,
		0x0e, 0x02, 0x00, 0x01, 0x00,
		0x0b,
		0x0b,
		0x0b,
	))
	m := decodeValidate(t, module(types, []uint32{0}, nil, nil, nil, [][]byte{body}))
	f, dump := buildOne(t, m)
	if !strings.Contains(dump, "switch %") || !strings.Contains(dump, "default:b") {
		t.Fatalf("unexpected br_table dump:\n%s", dump)
	}
	var sawSwitch bool
	for _, b := range f.Blocks {
		if b.Term.Kind != TermSwitch {
			continue
		}
		sawSwitch = true
		for i := uint32(0); i < b.Term.Edges.Len; i++ {
			e := f.Edges[b.Term.Edges.Start+i]
			if e.Args.Len != 2 {
				t.Fatalf("switch edge %d arg len = %d, want 2", i, e.Args.Len)
			}
			if f.Values[f.ValueIDs[e.Args.Start]].Type != wasm.I32 || f.Values[f.ValueIDs[e.Args.Start+1]].Type != wasm.I64 {
				t.Fatalf("switch edge %d arg types wrong", i)
			}
		}
	}
	if !sawSwitch {
		t.Fatal("no switch terminator found")
	}
}

func TestBuildEffectsForStatefulOps(t *testing.T) {
	glob := []global{{typ: wasm.GlobalType{Val: wasm.I32, Mutable: true}, init: bytes(0x41, 0x00, 0x0b)}}
	body := wasmtest.Code(bytes(
		0x20, 0x00, 0x22, 0x00,
		0x24, 0x00,
		0x23, 0x00,
		0x20, 0x01, 0x36, 0x02, 0x00,
		0x20, 0x01, 0x28, 0x02, 0x00,
		0x0b,
	))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}, glob, [][]byte{body}))
	f, _ := buildOne(t, m)
	seen := map[Op]EffectFlags{}
	for i := range f.Insts {
		seen[f.Insts[i].Op] |= f.Insts[i].Effects
	}
	checks := []struct {
		op   Op
		want EffectFlags
	}{
		{OpLocalTee, EffectReadLocal | EffectWriteLocal},
		{OpGlobalSet, EffectWriteGlobal},
		{OpGlobalGet, EffectReadGlobal},
		{OpStore, EffectCanTrap | EffectWriteMem},
		{OpLoad, EffectCanTrap | EffectReadMem},
	}
	for _, c := range checks {
		if got := seen[c.op]; got&c.want != c.want {
			t.Fatalf("%s effects = %v, want bits %v", opName(c.op), got, c.want)
		}
	}
}

func TestBuildBulkMemoryArgumentOrderAndEffects(t *testing.T) {
	body := wasmtest.Code(bytes(
		0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00,
		0x20, 0x00, 0x20, 0x03, 0x20, 0x02, 0xfc, 0x0b, 0x00,
		0x0b,
	))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}}}, []uint32{0}, nil, []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}, nil, [][]byte{body}))
	f, _ := buildOne(t, m)
	var copyInst, fillInst *Inst
	for i := range f.Insts {
		switch f.Insts[i].Op {
		case OpMemoryCopy:
			copyInst = &f.Insts[i]
		case OpMemoryFill:
			fillInst = &f.Insts[i]
		}
	}
	if copyInst == nil || fillInst == nil {
		t.Fatalf("missing memory.copy/fill in %+v", f.Insts)
	}
	if copyInst.Args.Len != 3 || fillInst.Args.Len != 3 {
		t.Fatalf("bulk arg counts copy=%d fill=%d", copyInst.Args.Len, fillInst.Args.Len)
	}
	if copyInst.Effects&(EffectCanTrap|EffectReadMem|EffectWriteMem) != (EffectCanTrap | EffectReadMem | EffectWriteMem) {
		t.Fatalf("copy effects=%v", copyInst.Effects)
	}
	if fillInst.Effects&(EffectCanTrap|EffectWriteMem) != (EffectCanTrap|EffectWriteMem) || fillInst.Effects&EffectReadMem != 0 {
		t.Fatalf("fill effects=%v", fillInst.Effects)
	}
}

func TestBuildModuleStopsOnBadLaterFunction(t *testing.T) {
	m := &wasm.Module{Types: []wasm.FuncType{{}}, Functions: []uint32{0, 0}, Code: []wasm.Code{{Body: bytes(0x0b)}, {Body: bytes(0xff, 0x0b)}}}
	_, err := BuildModule(m)
	if err == nil || !strings.Contains(err.Error(), "function 1") || !strings.Contains(err.Error(), "unsupported opcode") {
		t.Fatalf("BuildModule error = %v, want function 1 unsupported opcode", err)
	}
}

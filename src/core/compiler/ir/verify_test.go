package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestVerifyRejectsMissingTerminator(t *testing.T) {
	f := &Func{Sig: wasm.FuncType{}, Entry: 0, Blocks: []Block{{}}}
	if err := VerifyFunc(f); err == nil || !strings.Contains(err.Error(), "no terminator") {
		t.Fatalf("VerifyFunc error = %v, want missing terminator", err)
	}
}

func TestVerifyRejectsBadEdgeArity(t *testing.T) {
	f := &Func{Sig: wasm.FuncType{}, Entry: 0}
	f.Values = []Value{{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 1}}
	f.ValueIDs = []ValueID{0}
	f.Edges = []Edge{{To: 1}}
	f.Blocks = []Block{
		{Term: Term{Kind: TermBr, Edges: Range{Len: 1}}},
		{Params: Range{Len: 1}, Term: Term{Kind: TermReturn}},
	}
	if err := VerifyFunc(f); err == nil || !strings.Contains(err.Error(), "arg arity") {
		t.Fatalf("VerifyFunc error = %v, want arg arity", err)
	}
}

func TestVerifyRejectsReturnTypeMismatch(t *testing.T) {
	f := &Func{Sig: wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I64}}, Entry: 0, Locals: []wasm.ValType{wasm.I32}}
	f.Values = []Value{{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 0}}
	f.ValueIDs = []ValueID{0, 0}
	f.Blocks = []Block{{Params: Range{Start: 0, Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: 1, Len: 1}}}}
	if err := VerifyFunc(f); err == nil || !strings.Contains(err.Error(), "return arg") {
		t.Fatalf("VerifyFunc error = %v, want return type mismatch", err)
	}
}

func TestVerifyRejectsLoadWithoutTrapEffect(t *testing.T) {
	f := &Func{Sig: wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, Entry: 0, Locals: []wasm.ValType{wasm.I32}}
	f.Values = []Value{
		{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 0},
		{Type: wasm.I32, DefKind: ValueDefInst, Def: 0},
	}
	f.ValueIDs = []ValueID{0, 0, 1, 1}
	f.Insts = []Inst{{Op: OpLoad, Args: Range{Start: 1, Len: 1}, Results: Range{Start: 2, Len: 1}, Aux: packMem(MemI32, 2, 0, 0), Effects: EffectReadMem}}
	f.Blocks = []Block{{Params: Range{Start: 0, Len: 1}, Insts: Range{Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: 3, Len: 1}}}}
	if err := VerifyFunc(f); err == nil || !strings.Contains(err.Error(), "load missing effects") {
		t.Fatalf("VerifyFunc error = %v, want missing effect", err)
	}
}

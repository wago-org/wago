package ir

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestVerifyModuleRejectsWrappedU32TypeIndex(t *testing.T) {
	for _, index := range []uint32{0, 1, 2, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff} {
		t.Run(fmt.Sprintf("%08x", index), func(t *testing.T) {
			m := &Module{Types: []wasm.FuncType{{}, {}}, FuncTypes: []uint32{index}, ImportedFuncCount: 1}
			err := VerifyModule(m)
			if index < 2 {
				if err != nil {
					t.Fatalf("valid type index: %v", err)
				}
			} else {
				wantErr(t, err, fmt.Sprintf("function 0 has unknown type %d", index))
			}
		})
	}
}

func TestVerifyCallRejectsWrappedU32FunctionIndex(t *testing.T) {
	m := &Module{Types: []wasm.FuncType{{}}, FuncTypes: []uint32{0, 0}}
	for _, index := range []uint32{0, 1, 2, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff} {
		t.Run(fmt.Sprintf("%08x", index), func(t *testing.T) {
			err := verifyCall(m, 0, &Inst{Op: OpCall, Aux: uint64(index)}, 0, 0, nil, nil)
			if index < 2 {
				if err != nil {
					t.Fatalf("valid function index: %v", err)
				}
			} else {
				wantErr(t, err, fmt.Sprintf("call function %d out of range", index))
			}
		})
	}
}

func TestVerifyU32IndexBounds(t *testing.T) {
	for _, index := range []uint32{0, 1, 2, 3, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff} {
		t.Run(fmt.Sprintf("%08x", index), func(t *testing.T) {
			check := func(t *testing.T, err error, valid bool, want string) {
				t.Helper()
				if valid {
					if err != nil {
						t.Fatalf("valid index: %v", err)
					}
				} else {
					wantErr(t, err, want)
				}
			}
			t.Run("entry", func(t *testing.T) {
				f := &Func{Entry: BlockID(index), Blocks: []Block{{Term: Term{Kind: TermTrap}}, {Term: Term{Kind: TermTrap}}}}
				check(t, VerifyFunc(f), index < 2, fmt.Sprintf("entry block %d out of range", index))
			})
			t.Run("edge", func(t *testing.T) {
				f := &Func{Blocks: []Block{{Term: Term{Kind: TermBr, Edges: Range{Len: 1}}}, {Term: Term{Kind: TermTrap}}}, Edges: []Edge{{To: BlockID(index)}}}
				check(t, VerifyFunc(f), index < 2, fmt.Sprintf("edge 0 target %d out of range", index))
			})
			t.Run("block definition", func(t *testing.T) {
				f := validReturnI32Func()
				f.Values[0].Def = index
				check(t, VerifyFunc(f), index == 0, fmt.Sprintf("invalid block def %d", index))
			})
			t.Run("instruction definition", func(t *testing.T) {
				f := validConstReturnI32Func()
				f.Values[0].Def = index
				check(t, VerifyFunc(f), index == 0, fmt.Sprintf("invalid inst def %d", index))
			})
			t.Run("value", func(t *testing.T) {
				f := validReturnI32Func()
				f.ValueIDs[1] = ValueID(index)
				check(t, VerifyFunc(f), index == 0, fmt.Sprintf("return args invalid value %d", index))
			})
			t.Run("import count", func(t *testing.T) {
				m := &Module{Types: []wasm.FuncType{{}}, FuncTypes: []uint32{0, 0}, ImportedFuncCount: index}
				if index <= 2 {
					for i := index; i < 2; i++ {
						m.Funcs = append(m.Funcs, Func{Index: i, LocalIndex: i - index, Blocks: []Block{{Term: Term{Kind: TermReturn}}}})
					}
				}
				check(t, VerifyModule(m), index <= 2, fmt.Sprintf("imported function count %d exceeds function type count 2", index))
			})
			t.Run("call type", func(t *testing.T) {
				m := &Module{Types: []wasm.FuncType{{}, {}}, FuncTypes: []uint32{index}}
				err := verifyCall(m, 0, &Inst{Op: OpCall}, 0, 0, nil, nil)
				check(t, err, index < 2, fmt.Sprintf("call function 0 has unknown type %d", index))
			})
			t.Run("builder function", func(t *testing.T) {
				b := &Builder{out: &Module{FuncTypes: []uint32{0, 0}}}
				_, err := b.funcTypeIndex(index)
				check(t, err, index < 2, fmt.Sprintf("unknown function %d", index))
			})

			t.Run("builder label", func(t *testing.T) {
				b := &Builder{labels: []label{{}, {}}}
				_, err := b.labelAt(index)
				check(t, err, index < 2, fmt.Sprintf("unknown label depth %d", index))
			})
		})
	}
}

func BenchmarkVerifyCallFunctionBounds(b *testing.B) {
	m := &Module{Types: []wasm.FuncType{{}}, FuncTypes: []uint32{0}}
	in := &Inst{Op: OpCall, Aux: 0}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := verifyCall(m, 0, in, 0, 0, nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}

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

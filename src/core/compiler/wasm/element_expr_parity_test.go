package wasm

import "testing"

func TestElementNullRepresentationParity(t *testing.T) {
	for _, heap := range []AbsHeapType{HeapFunc, HeapNoFunc, HeapExtern, HeapNoExtern} {
		t.Run(RefVal(AbsRef(heap)).String(), func(t *testing.T) {
			body := Expr{BodyBytes: []byte{0xd0, byte(heap), 0x0b}}
			want, err := ParseElementExpr(body)
			if err != nil {
				t.Fatal(err)
			}
			for _, ref := range []RefType{AbsRef(heap), Ref(true, AbsHeap(heap), false)} {
				ast := Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: ref}}}}
				got, err := ParseElementExpr(ast)
				if err != nil || got != want {
					t.Fatalf("AST (%s, bare=%v) = %+v, %v; bytes = %+v", ref, ref.Bare(), got, err, want)
				}
				gotFunc, gotErr := ParseFuncrefElementExpr(ast)
				wantFunc, wantErr := ParseFuncrefElementExpr(body)
				if gotFunc != wantFunc || (gotErr == nil) != (wantErr == nil) {
					t.Fatalf("funcref AST = %+v, %v; bytes = %+v, %v", gotFunc, gotErr, wantFunc, wantErr)
				}
			}
		})
	}
}

func TestElementNullRejectsOtherASTTypes(t *testing.T) {
	for _, ref := range []RefType{AbsRef(HeapAny), Ref(false, AbsHeap(HeapFunc), false), Ref(true, AbsHeap(HeapFunc), true), Ref(true, IndexedHeap(TypeIdx{}), false)} {
		if _, err := ParseElementExpr(Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: ref}}}}); err == nil {
			t.Fatalf("accepted unsupported type %s", RefVal(ref))
		}
	}
}

func BenchmarkElementNullParsing(b *testing.B) {
	for _, heap := range []AbsHeapType{HeapFunc, HeapExtern} {
		for _, ast := range []bool{false, true} {
			expr := Expr{BodyBytes: []byte{0xd0, byte(heap), 0x0b}}
			name := RefVal(AbsRef(heap)).String() + "/bytes"
			if ast {
				expr = Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(heap)}}}}
				name = RefVal(AbsRef(heap)).String() + "/AST"
			}
			b.Run(name, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if got, err := ParseElementExpr(expr); err != nil || !got.Null {
						b.Fatalf("parse = %+v, %v", got, err)
					}
				}
			})
		}
	}
}

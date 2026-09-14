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
			canonical := FuncRef.Ref()
			if heap == HeapExtern || heap == HeapNoExtern {
				canonical = ExternRef.Ref()
			}
			if want != (ElementExpr{RefType: canonical, Null: true}) {
				t.Fatalf("bytes = %+v; want canonical null %s", want, canonical)
			}
			decoded, err := decodeExpr(newReader(body.BodyBytes), 0)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := ParseElementExpr(decoded); err != nil || got != want {
				t.Fatalf("decoded AST = %+v, %v; bytes = %+v", got, err, want)
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
	refs := []RefType{
		{},
		Ref(true, IndexedHeap(TypeIdx{}), false),
		Ref(true, IndexedHeap(TypeIdx{Rec: true, Index: 1}), false),
		Ref(true, DefinedHeap(nil), false),
	}
	for _, heap := range []AbsHeapType{HeapString, HeapExn, HeapArray, HeapStruct, HeapI31, HeapEq, HeapAny, HeapNone, HeapNoExn} {
		refs = append(refs, AbsRef(heap), Ref(true, AbsHeap(heap), false))
	}
	for _, heap := range []AbsHeapType{HeapFunc, HeapNoFunc, HeapExtern, HeapNoExtern} {
		refs = append(refs, Ref(false, AbsHeap(heap), false), Ref(true, AbsHeap(heap), true), Ref(false, AbsHeap(heap), true))
	}
	for _, ref := range refs {
		t.Run(ref.String(), func(t *testing.T) {
			expr := Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: ref}}}}
			if got, err := ParseElementExpr(expr); err == nil || got != (ElementExpr{}) {
				t.Fatalf("unsupported type %s: got %+v, %v", ref, got, err)
			}
			if got, err := ParseFuncrefElementExpr(expr); err == nil || got != (FuncrefElementExpr{}) {
				t.Fatalf("unsupported funcref type %s: got %+v, %v", ref, got, err)
			}
		})
	}
}

func BenchmarkElementNullParsing(b *testing.B) {
	for _, heap := range []AbsHeapType{HeapFunc, HeapExtern} {
		for _, form := range []string{"bytes", "AST", "expanded"} {
			expr := Expr{BodyBytes: []byte{0xd0, byte(heap), 0x0b}}
			name := RefVal(AbsRef(heap)).String() + "/" + form
			if form != "bytes" {
				ref := AbsRef(heap)
				if form == "expanded" {
					ref = Ref(true, AbsHeap(heap), false)
				}
				expr = Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: ref}}}}
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

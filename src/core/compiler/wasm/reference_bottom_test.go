package wasm

import "testing"

func TestReferenceInstructionsConstrainBottom(t *testing.T) {
	for _, kind := range []InstrKind{InstrRefAsNonNull, InstrBrOnNull} {
		for _, want := range []ValType{I32, I64, F32, F64, V128, FuncRef, ExternRef, AnyRef, RefVal(AbsRef(HeapExn).WithNullable(false))} {
			t.Run(kind.String()+"/"+want.String(), func(t *testing.T) {
				v := coverageFuncValidator(&Module{}, nil)
				v.unreachable()
				if err := v.step(&Instruction{Kind: kind}); err != nil {
					t.Fatal(err)
				}
				err := v.popExpect(want)
				if want.Kind() == ValRef {
					if err != nil {
						t.Fatalf("reference bottom must match %v: %v", want, err)
					}
				} else if err == nil {
					t.Fatalf("reference bottom matched %v", want)
				}
			})
		}
	}
}

func TestReferenceBottomSupportsCasts(t *testing.T) {
	v := coverageFuncValidator(&Module{}, nil)
	bottom := nonNullValidationType(val{unknown: true}).Ref()
	for _, target := range []RefType{AbsRef(HeapFunc), AbsRef(HeapExtern), AbsRef(HeapAny)} {
		if !v.refTestCompatible(bottom, target) || !v.refSubtype(bottom, target) {
			t.Fatalf("reference bottom does not match %v", target)
		}
	}
}

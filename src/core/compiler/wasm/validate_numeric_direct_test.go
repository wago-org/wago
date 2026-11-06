package wasm

import (
	"reflect"
	"testing"
)

func TestDirectNumericStackEffectParity(t *testing.T) {
	types := []ValType{I32, I64, F32, F64, V128, ValType{}}
	for opcode := 0x45; opcode <= 0xc4; opcode++ {
		kind := simpleOpcode[opcode]
		for _, unreachable := range []bool{false, true} {
			for height := 0; height < 2; height++ {
				for n := 0; n < 4; n++ {
					for _, a := range types {
						for _, b := range types {
							for unknownMask := 0; unknownMask < 4; unknownMask++ {
								values := make([]val, n+height)
								for i := range values {
									values[i] = val{t: a}
									if i%2 != 0 {
										values[i].t = b
									}
								}
								// Either operand can be polymorphic, independently of the
								// other operand and the surrounding control-stack prefix.
								for operand := 0; operand < 2 && operand < len(values); operand++ {
									if unknownMask&(1<<operand) != 0 {
										values[len(values)-1-operand].t.lo |= unknownValidationBit
									}
								}
								frame := ctrlFrame{height: height, unreachable: unreachable}
								reference := funcValidator{ctrls: []ctrlFrame{frame}, vals: append([]val(nil), values...)}
								direct := funcValidator{ctrls: []ctrlFrame{frame}, vals: append([]val(nil), values...)}
								want := reference.step(&Instruction{Kind: kind})
								got := direct.stepDirectNumeric(kind)
								if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(direct.vals, reference.vals) {
									t.Fatalf("opcode %#x unreachable=%t height=%d unknownMask=%d stack=%v: direct=%v/%v reference=%v/%v", opcode, unreachable, height, unknownMask, values, got, direct.vals, want, reference.vals)
								}
							}
						}
					}
				}
			}
		}
	}
}

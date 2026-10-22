//go:build linux && !tinygo

package riscv64

type swarScalarFloat struct {
	input, output       swarClass
	unary, fp, trapping bool
}

// Width alone cannot distinguish integer conversion operands, FP carriers
// and raw reinterpretation. Preserve the exact classes across scalar/vector
// composition even though each scalar occupies the same padded stack home.
func swarScalarFloatOpcode(op byte) (swarScalarFloat, bool) {
	wide, unary, resultWide, ok := floatOpcode(op)
	if !ok {
		return swarScalarFloat{}, false
	}
	c := swarScalarFloat{input: swarF32, output: swarF32, unary: unary, fp: true}
	if wide {
		c.input = swarF64
	}
	if resultWide {
		c.output = swarF64
	}
	switch {
	case op >= 0x5b && op <= 0x66:
		c.output = swarI32
	case op >= 0xa8 && op <= 0xb1:
		c.output, c.trapping = swarI32, true
		if resultWide {
			c.output = swarI64
		}
	case op >= 0xb2 && op <= 0xba && op != 0xb6:
		c.input = swarI32
		if wide {
			c.input = swarI64
		}
	case op == 0xbc:
		c.output = swarI32
	case op == 0xbd:
		c.output = swarI64
	case op == 0xbe:
		c.input = swarI32
	case op == 0xbf:
		c.input = swarI64
	}
	if op >= 0xbc || op == 0x8b || op == 0x8c || op == 0x98 || op == 0x99 || op == 0x9a || op == 0xa6 {
		c.fp = false // Raw sign/reinterpret operations use GP instructions only.
	}
	return c, true
}

func swarSaturatingFloatOpcode(sub uint32) (swarScalarFloat, bool) {
	if sub > 7 {
		return swarScalarFloat{}, false
	}
	c := swarScalarFloat{input: swarF32, output: swarI32, unary: true, fp: true}
	if sub&2 != 0 {
		c.input = swarF64
	}
	if sub&4 != 0 {
		c.output = swarI64
	}
	return c, true
}

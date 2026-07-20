package embedded32

import "testing"

func TestI32DivRemOpcodeRegistry(t *testing.T) {
	for opcode := byte(0x6d); opcode <= 0x70; opcode++ {
		op, ok := I32DivRemForWasmOpcode(opcode)
		if !ok || byte(op) != opcode-0x6d {
			t.Fatalf("opcode %#x: op=%d ok=%t", opcode, op, ok)
		}
		signed, remainder, valid := I32DivRemInfo(op)
		if !valid || signed != (opcode == 0x6d || opcode == 0x6f) || remainder != (opcode >= 0x6f) {
			t.Fatalf("opcode %#x: signed=%t remainder=%t valid=%t", opcode, signed, remainder, valid)
		}
	}
	if _, ok := I32DivRemForWasmOpcode(0x6c); ok {
		t.Fatal("non-div/rem opcode accepted")
	}
	if _, _, ok := I32DivRemInfo(255); ok {
		t.Fatal("invalid div/rem operation accepted")
	}
}

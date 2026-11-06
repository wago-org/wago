//go:build amd64

package amd64

import "encoding/binary"

// scalarLoopNopLen decodes NOP and multi-byte NOP /0, including its complete
// addressing fields. NOP does not access the address denoted by its operand.
func scalarLoopNopLen(b []byte) int {
	p := 0
	for p < len(b) && b[p] == 0x66 && p < 12 {
		p++
	}
	if p >= len(b) {
		return 0
	}
	if b[p] == 0x90 {
		return p + 1
	}
	if len(b)-p < 3 || b[p] != 0x0f || b[p+1] != 0x1f {
		return 0
	}
	m := b[p+2]
	if (m>>3)&7 != 0 {
		return 0
	}
	p += 3
	mod, base := m>>6, m&7
	if mod != 3 && base == 4 {
		if p == len(b) {
			return 0
		}
		base = b[p] & 7
		p++
	}
	displacement := 0
	if mod == 1 {
		displacement = 1
	}
	if mod == 2 || mod == 0 && base == 5 {
		displacement = 4
	}
	if len(b)-p < displacement || p+displacement > 15 {
		return 0
	}
	return p + displacement
}

// scalarLoopStackStore64 admits only MOV64 [RSP+disp], GP, with no index.
func scalarLoopStackStore64(b []byte) (Reg, int32, int, bool) {
	if len(b) < 4 || b[0] != 0x48 && b[0] != 0x4c || b[1] != 0x89 {
		return regNone, 0, 0, false
	}
	m := b[2]
	if m>>6 == 3 || m&7 != 4 || b[3] != 0x24 {
		return regNone, 0, 0, false
	}
	source, p := Reg((m>>3)&7)|Reg(b[0]&4)<<1, 4
	var disp int32
	switch m >> 6 {
	case 1:
		if len(b)-p < 1 {
			return regNone, 0, 0, false
		}
		disp, p = int32(int8(b[p])), p+1
	case 2:
		if len(b)-p < 4 {
			return regNone, 0, 0, false
		}
		disp, p = int32(binary.LittleEndian.Uint32(b[p:])), p+4
	}
	return source, disp, p, true
}

func scalarLoopZeroCounter(b []byte) (Reg, bool, bool) {
	p, rex := 0, byte(0)
	if len(b) != 0 && b[0]&0xf0 == 0x40 {
		p, rex = 1, b[0]
	}
	if len(b)-p != 2 || rex&2 != 0 || b[p] != 0x85 || b[p+1]>>6 != 3 {
		return regNone, false, false
	}
	m := b[p+1]
	left := Reg((m>>3)&7) | Reg((rex>>2)&1)<<3
	right := Reg(m&7) | Reg(rex&1)<<3
	return left, rex&8 != 0, left == right && left != RSP
}

func scalarLoopUnitDecrement(b []byte, counter Reg, wide bool) bool {
	p, rex := 0, byte(0)
	if len(b) != 0 && b[0]&0xf0 == 0x40 {
		p, rex = 1, b[0]
	}
	if len(b)-p < 3 || rex&6 != 0 || (rex&8 != 0) != wide {
		return false
	}
	op, m := b[p], b[p+1]
	if m>>6 != 3 || (m>>3)&7 != 5 || Reg(m&7)|Reg(rex&1)<<3 != counter {
		return false
	}
	if op == 0x83 {
		return len(b)-p == 3 && int8(b[p+2]) == 1
	}
	if op == 0x81 {
		return len(b)-p == 6 && int32(binary.LittleEndian.Uint32(b[p+2:])) == 1
	}
	return false
}

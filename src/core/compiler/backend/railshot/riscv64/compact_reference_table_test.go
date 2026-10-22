//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func rv64HasInstruction(code []byte, want uint32) bool {
	for len(code) >= 4 {
		if binary.LittleEndian.Uint32(code) == want {
			return true
		}
		code = code[4:]
	}
	return false
}

func rv64IType(opcode, funct3, rd, rs1, imm uint32) uint32 {
	return imm<<20 | rs1<<15 | funct3<<12 | rd<<7 | opcode
}

func rv64SType(opcode, funct3, rs1, rs2, imm uint32) uint32 {
	return (imm>>5)<<25 | rs2<<20 | rs1<<15 | funct3<<12 | (imm&31)<<7 | opcode
}

func TestCompactReferenceTableGetSetLowering(t *testing.T) {
	ft := &wasm.CompType{
		Kind:    wasm.CompFunc,
		Params:  []wasm.ValType{wasm.I32, wasm.ExternRef},
		Results: []wasm.ValType{wasm.I32},
	}
	fn := &wasm.Func{BodyBytes: []byte{
		0x20, 0, 0x20, 1, 0x26, 0, // table.set 0
		0x20, 0, 0x25, 0, 0xd1, 0x0b, // ref.is_null (table.get 0)
	}}
	state := moduleState{
		production:            true,
		referenceFunctions:    true,
		referenceCalls:        true,
		importReferences:      true,
		ownedTable:            true,
		tableAddr64:           []bool{false},
		tableCompactRef:       []bool{true},
		declarationExternRefs: true,
		types:                 []functionSpec{{signature: ft}},
		functions:             []functionSpec{{signature: ft}},
	}
	summary, err := walkInteger(fn, ft, &state, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, _, _, err := compileIntegerMode(fn, ft, &state, summary, emissionMode{production: true, referenceFunctions: true})
	if err != nil {
		t.Fatal(err)
	}
	// Compact rows use index<<3, then load/store the complete opaque carrier at
	// row offset zero. In particular, they never dereference or copy a 32-byte
	// callable descriptor from an externref token.
	for name, instruction := range map[string]uint32{
		"index-times-eight": rv64IType(0x13, 1, 5, 5, 3),
		"full-width-load":   rv64IType(0x03, 3, 5, 6, 0),
		"full-width-store":  rv64SType(0x23, 3, 6, 28, 0),
	} {
		if !rv64HasInstruction(code, instruction) {
			t.Fatalf("compact %s instruction missing: %#08x", name, instruction)
		}
	}
	if rv64HasInstruction(code, rv64IType(0x13, 1, 5, 5, 5)) || rv64HasInstruction(code, rv64IType(0x03, 3, 5, 6, 24)) {
		t.Fatal("compact reference lowering used funcref descriptor row layout")
	}
}

func TestReferenceTablePhysicalLayoutDirectory(t *testing.T) {
	state := moduleState{ownedTable: true, tableAddr64: []bool{false, false, false}, tableCompactRef: []bool{false, true, false}}
	for index, want := range []bool{false, true, false} {
		got, ok := state.tableCompactReference(uint32(index))
		if !ok || got != want {
			t.Fatalf("table %d compact = %t, %t; want %t, true", index, got, ok, want)
		}
	}
	if _, ok := state.tableCompactReference(3); ok {
		t.Fatal("out-of-range compact table layout admitted")
	}
	legacy := moduleState{ownedTable: true}
	if compact, ok := legacy.tableCompactReference(0); !ok || compact {
		t.Fatal("legacy single funcref table layout changed", compact, ok)
	}
}

//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// A direct integer adapter consumes A0..A3 instead of an argument pointer. The
// Engine has already lent its owned invocation view; the adapter validates the
// complete areas before writing and tail-enters the existing framed function.
func directIntegerAdapter(delta int64, params, results int) []byte {
	return directIntegerAdapterInto(make([]byte, 0, 384), delta, params, results)
}

func directIntegerAdapterInto(storage []byte, delta int64, params, results int) []byte {
	if params < 0 || params > 4 || results < 0 || results > 1 {
		return nil
	}
	a := enc.Asm{B: storage[:0]}
	a.Ld(enc.X28, enc.X25, -abi.RV64InvocationViewOffset)
	var guardStorage [32]int
	bad := append(guardStorage[:0], a.BranchLong(enc.EQ, enc.X28, enc.Zero))
	bad = nativeInvocationViewGuardsInto(&a, enc.X28, 0, bad)
	a.Ld(enc.X29, enc.X28, abi.RV64InvocationArgumentBytesOffset)
	a.Constant32(enc.X30, uint32(params*8))
	bad = append(bad, a.BranchLong(enc.LTU, enc.X29, enc.X30))
	a.Ld(enc.X29, enc.X28, abi.RV64InvocationArgumentsOffset)
	if params != 0 {
		bad = append(bad, a.BranchLong(enc.EQ, enc.X29, enc.Zero))
	}
	a.Ld(enc.X30, enc.X28, abi.RV64InvocationResultBytesOffset)
	a.Constant32(enc.X31, uint32(results*8))
	bad = append(bad, a.BranchLong(enc.LTU, enc.X30, enc.X31))
	if results != 0 {
		a.Ld(enc.X30, enc.X28, abi.RV64InvocationResultsOffset)
		bad = append(bad, a.BranchLong(enc.EQ, enc.X30, enc.Zero))
	}
	for i := 0; i < params; i++ {
		a.Sd(enc.Reg(10+i), enc.X29, int32(i*8))
	}
	a.Addi(enc.X10, enc.X29, 0)
	a.Addi(enc.X11, enc.X25, 0)
	a.Ld(enc.X12, enc.X25, -abi.TrapCellPtrOffset)
	a.Ld(enc.X13, enc.X28, abi.RV64InvocationResultsOffset)
	jump := a.JumpLong(false)
	if a.PatchLong(jump, delta) != nil {
		return nil
	}
	fail := a.Len()
	for _, site := range bad {
		if a.PatchLong(site, int64(fail)) != nil {
			return nil
		}
	}
	a.Ld(enc.X12, enc.X25, -abi.TrapCellPtrOffset)
	a.Constant32(enc.X5, abi.TrapCalledFnNotLinked)
	a.Sw(enc.X5, enc.X12, 0)
	a.Ld(enc.SP, enc.X25, -abi.RV64TrapReentrySPOffset)
	a.Ld(enc.X31, enc.X25, -abi.RV64TrapContinuationOffset)
	a.Jalr(enc.Zero, enc.X31, 0)
	if a.Err() != nil {
		return nil
	}
	return a.B
}

// NativeDirectIntegerEntry recognizes only the exact checked adapter targeting
// the same ordinary wrapper. Compiler-only selection markers are checked by the
// public caller; this byte identity also remains usable for trusted artifacts.
func NativeDirectIntegerEntry(code []byte, direct, framed, params, results int) bool {
	if direct < 0 || framed < 0 || direct&15 != 0 || framed&15 != 0 || direct >= len(code) || framed >= direct {
		return false
	}
	var storage [384]byte
	expected := directIntegerAdapterInto(storage[:0], int64(framed-direct), params, results)
	return len(expected) != 0 && len(expected) <= len(code)-direct && bytes.Equal(code[direct:direct+len(expected)], expected)
}

func directIntegerSignature(ft *wasm.CompType) bool {
	if ft == nil || len(ft.Params) > 4 || len(ft.Results) > 1 {
		return false
	}
	for _, list := range [][]wasm.ValType{ft.Params, ft.Results} {
		for _, typ := range list {
			if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) {
				return false
			}
		}
	}
	return true
}
func boundedDirectInteger(fn *wasm.Func, code []byte, frame int) bool {
	if fn == nil || len(fn.BodyBytes) > 256 || len(code) > 4096 || frame > 4096 {
		return false
	}
	r := wasm.ReaderFrom(fn.BodyBytes)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x03 || op >= 0x10 && op <= 0x15 || op == 0x23 || op == 0x24 || op >= 0x28 && op <= 0x40 || op >= 0xd0 {
			return false
		}
		if wasm.SkipInstructionImmediate(&r, op) != nil {
			return false
		}
	}
	for at := 0; at < len(code); at += 4 {
		w := binary.LittleEndian.Uint32(code[at:])
		op := w & 127
		rd := w >> 7 & 31
		if (op == 0x6f || op == 0x67) && rd == 1 {
			return false
		}
		if op == 0x63 {
			imm := (w>>31)<<12 | (w>>7&1)<<11 | (w>>25&63)<<5 | (w>>8&15)<<1
			if int32(imm<<19)>>19 < 0 {
				return false
			}
		}
	}
	return true
}

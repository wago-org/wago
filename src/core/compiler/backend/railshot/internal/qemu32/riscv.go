package qemu32

import (
	"bytes"
	"encoding/binary"
	"fmt"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func RVELF(image []byte) ([]byte, Layout, error) {
	layout := DataLayout(uint32(len(image)))
	main, err := rvMain(layout)
	if err != nil {
		return nil, Layout{}, err
	}
	code := make([]byte, ImageBase-CodeBase)
	copy(code, main)
	for _, helper := range []struct {
		address uint32
		kind    uint32
		size    uint32
	}{
		{RVF32Helper, 0, embedded32.F32FrameBytes},
		{RVF64Helper, 1, embedded32.F64FrameBytes},
		{RVI64Helper, 2, embedded32.I64FrameBytes},
		{RVSIMDHelper, 3, embedded32.SIMDFrameBytes},
	} {
		body, err := rvHelper(layout, helper.kind, helper.size)
		if err != nil {
			return nil, Layout{}, err
		}
		offset := helper.address - CodeBase
		if uint64(offset)+uint64(len(body)) > uint64(len(code)) {
			return nil, Layout{}, fmt.Errorf("qemu32: rv helper %#x exceeds driver region", helper.address)
		}
		copy(code[offset:], body)
	}
	code = append(code, image...)
	end := layout.HelperStateAddress + 8
	for CodeBase+uint32(len(code)) < end {
		code = append(code, 0)
	}
	return rvELF(code), layout, nil
}

func rvMain(layout Layout) ([]byte, error) {
	var a rv.Asm
	emitRead := func(address uint32, countReg rv.Reg, count uint32) {
		a.MovImm32(rv.A0, 0)
		a.MovImm32(rv.A1, address)
		if countReg != rv.Zero {
			a.MovReg(rv.A2, countReg)
		} else {
			a.MovImm32(rv.A2, count)
		}
		a.MovImm32(rv.A7, 63)
		a.Ecall()
	}
	emitWrite := func(address uint32, countReg rv.Reg, count uint32) {
		a.MovImm32(rv.A0, 1)
		a.MovImm32(rv.A1, address)
		if countReg != rv.Zero {
			a.MovReg(rv.A2, countReg)
		} else {
			a.MovImm32(rv.A2, count)
		}
		a.MovImm32(rv.A7, 64)
		a.Ecall()
	}
	loop := a.Len()
	emitRead(layout.RequestAddress, rv.Zero, 20)
	a.MovImm32(rv.T2, layout.RequestAddress)
	if !a.Lw(rv.T0, rv.T2, 0) {
		return nil, fmt.Errorf("qemu32: encode rv request op")
	}
	a.MovImm32(rv.T1, protocolCall)
	callBranch := a.FarBcond(rv.T0, rv.T1, rv.CondEQ, rv.T6)
	a.MovImm32(rv.T1, protocolStart)
	startBranch := a.FarBcond(rv.T0, rv.T1, rv.CondEQ, rv.T6)
	a.MovImm32(rv.T1, protocolRead)
	readBranch := a.FarBcond(rv.T0, rv.T1, rv.CondEQ, rv.T6)
	a.MovImm32(rv.T1, protocolWrite)
	writeBranch := a.FarBcond(rv.T0, rv.T1, rv.CondEQ, rv.T6)
	a.MovImm32(rv.T1, protocolExit)
	exitBranch := a.FarBcond(rv.T0, rv.T1, rv.CondEQ, rv.T6)
	a.MovImm32(rv.A0, 126)
	a.MovImm32(rv.A7, 93)
	a.Ecall()

	callTarget := a.Len()
	a.MovImm32(rv.T2, layout.RequestAddress)
	if !a.Lw(rv.S0, rv.T2, 12) || !a.Lw(rv.S1, rv.T2, 16) {
		return nil, fmt.Errorf("qemu32: encode rv call counts")
	}
	parametersReady := a.FarBcond(rv.S0, rv.Zero, rv.CondEQ, rv.T6)
	a.Slli(rv.T1, rv.S0, 2)
	emitRead(layout.RequestAddress+20, rv.T1, 0)
	if !a.PatchFarBranch(parametersReady, a.Len()) {
		return nil, fmt.Errorf("qemu32: patch rv parameter branch")
	}
	a.MovImm32(rv.T2, layout.RequestAddress)
	if !a.Lw(rv.T0, rv.T2, 8) {
		return nil, fmt.Errorf("qemu32: encode rv call context")
	}
	if !a.Lw(rv.T3, rv.T0, embedded32.ContextTrapCellOffset) || !a.Sw(rv.Zero, rv.T3, 0) {
		return nil, fmt.Errorf("qemu32: encode rv trap clear")
	}
	a.MovImm32(rv.T3, 64*1024)
	a.Sub(rv.T3, rv.SP, rv.T3)
	if !a.Sw(rv.T3, rv.T0, embedded32.ContextStackLimitOffset) {
		return nil, fmt.Errorf("qemu32: encode rv stack limit")
	}
	a.MovImm32(rv.T1, layout.CallAddress)
	if !a.Sw(rv.T0, rv.T1, embedded32.CallABIContextOffset) {
		return nil, fmt.Errorf("qemu32: encode rv call context store")
	}
	a.MovImm32(rv.T0, layout.RequestAddress+20)
	if !a.Sw(rv.T0, rv.T1, embedded32.CallABIParametersOffset) {
		return nil, fmt.Errorf("qemu32: encode rv parameters store")
	}
	a.MovImm32(rv.T0, layout.ResponseAddress+12)
	if !a.Sw(rv.T0, rv.T1, embedded32.CallABIResultsOffset) {
		return nil, fmt.Errorf("qemu32: encode rv results store")
	}
	if !a.Lw(rv.T0, rv.T2, 4) {
		return nil, fmt.Errorf("qemu32: encode rv call entry")
	}
	a.MovReg(rv.A0, rv.T1)
	a.Blr(rv.T0)
	a.MovImm32(rv.T2, layout.ResponseAddress)
	if !a.Sw(rv.A0, rv.T2, 4) {
		return nil, fmt.Errorf("qemu32: encode rv call code")
	}
	a.MovImm32(rv.T1, protocolResult)
	if !a.Sw(rv.T1, rv.T2, 0) {
		return nil, fmt.Errorf("qemu32: encode rv result tag")
	}
	trapped := a.FarBcond(rv.A0, rv.Zero, rv.CondNE, rv.T6)
	if !a.Sw(rv.S1, rv.T2, 8) {
		return nil, fmt.Errorf("qemu32: encode rv result count")
	}
	resultCountReady := a.FarJump(rv.Zero, rv.T6)
	trappedTarget := a.Len()
	if !a.Sw(rv.Zero, rv.T2, 8) {
		return nil, fmt.Errorf("qemu32: encode rv trapped count")
	}
	a.MovReg(rv.S1, rv.Zero)
	resultCountTarget := a.Len()
	if !a.PatchFarBranch(trapped, trappedTarget) || !a.PatchFarJump(resultCountReady, resultCountTarget) {
		return nil, fmt.Errorf("qemu32: patch rv result count")
	}
	emitWrite(layout.ResponseAddress, rv.Zero, 12)
	noResults := a.FarBcond(rv.S1, rv.Zero, rv.CondEQ, rv.T6)
	a.Slli(rv.T1, rv.S1, 2)
	emitWrite(layout.ResponseAddress+12, rv.T1, 0)
	if !a.PatchFarBranch(noResults, a.Len()) {
		return nil, fmt.Errorf("qemu32: patch rv no-results branch")
	}
	back := a.FarJump(rv.Zero, rv.T6)
	if !a.PatchFarJump(back, loop) {
		return nil, fmt.Errorf("qemu32: patch rv call loop")
	}

	startTarget := a.Len()
	a.MovImm32(rv.T2, layout.RequestAddress)
	if !a.Lw(rv.T0, rv.T2, 4) || !a.Lw(rv.A0, rv.T2, 8) {
		return nil, fmt.Errorf("qemu32: encode rv start request")
	}
	if !a.Lw(rv.T3, rv.A0, embedded32.ContextTrapCellOffset) || !a.Sw(rv.Zero, rv.T3, 0) {
		return nil, fmt.Errorf("qemu32: encode rv start trap clear")
	}
	a.MovImm32(rv.T3, 64*1024)
	a.Sub(rv.T3, rv.SP, rv.T3)
	if !a.Sw(rv.T3, rv.A0, embedded32.ContextStackLimitOffset) {
		return nil, fmt.Errorf("qemu32: encode rv start stack limit")
	}
	a.Blr(rv.T0)
	a.MovImm32(rv.T2, layout.ResponseAddress)
	a.MovImm32(rv.T1, protocolResult)
	if !a.Sw(rv.T1, rv.T2, 0) || !a.Sw(rv.A0, rv.T2, 4) || !a.Sw(rv.Zero, rv.T2, 8) {
		return nil, fmt.Errorf("qemu32: encode rv start result")
	}
	emitWrite(layout.ResponseAddress, rv.Zero, 12)
	back = a.FarJump(rv.Zero, rv.T6)
	if !a.PatchFarJump(back, loop) {
		return nil, fmt.Errorf("qemu32: patch rv start loop")
	}

	readTarget := a.Len()
	a.MovImm32(rv.T2, layout.RequestAddress)
	if !a.Lw(rv.S0, rv.T2, 4) || !a.Lw(rv.S1, rv.T2, 16) {
		return nil, fmt.Errorf("qemu32: encode rv read request")
	}
	a.MovImm32(rv.T2, layout.ResponseAddress)
	a.MovImm32(rv.T1, protocolResult)
	if !a.Sw(rv.T1, rv.T2, 0) || !a.Sw(rv.Zero, rv.T2, 4) || !a.Sw(rv.S1, rv.T2, 8) {
		return nil, fmt.Errorf("qemu32: encode rv read response")
	}
	emitWrite(layout.ResponseAddress, rv.Zero, 12)
	a.Slli(rv.A2, rv.S1, 2)
	a.MovImm32(rv.A0, 1)
	a.MovReg(rv.A1, rv.S0)
	a.MovImm32(rv.A7, 64)
	a.Ecall()
	back = a.FarJump(rv.Zero, rv.T6)
	if !a.PatchFarJump(back, loop) {
		return nil, fmt.Errorf("qemu32: patch rv read loop")
	}

	writeTarget := a.Len()
	a.MovImm32(rv.T2, layout.RequestAddress)
	if !a.Lw(rv.A1, rv.T2, 4) || !a.Lw(rv.A2, rv.T2, 16) {
		return nil, fmt.Errorf("qemu32: encode rv write request")
	}
	a.MovImm32(rv.A0, 0)
	a.MovImm32(rv.A7, 63)
	a.Ecall()
	a.MovImm32(rv.T2, layout.ResponseAddress)
	a.MovImm32(rv.T1, protocolResult)
	if !a.Sw(rv.T1, rv.T2, 0) || !a.Sw(rv.Zero, rv.T2, 4) || !a.Sw(rv.Zero, rv.T2, 8) {
		return nil, fmt.Errorf("qemu32: encode rv write response")
	}
	emitWrite(layout.ResponseAddress, rv.Zero, 12)
	back = a.FarJump(rv.Zero, rv.T6)
	if !a.PatchFarJump(back, loop) {
		return nil, fmt.Errorf("qemu32: patch rv write loop")
	}

	exitTarget := a.Len()
	a.MovImm32(rv.A0, 0)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchFarBranch(callBranch, callTarget) || !a.PatchFarBranch(startBranch, startTarget) || !a.PatchFarBranch(readBranch, readTarget) || !a.PatchFarBranch(writeBranch, writeTarget) || !a.PatchFarBranch(exitBranch, exitTarget) {
		return nil, fmt.Errorf("qemu32: patch rv dispatch")
	}
	return a.B, nil
}

func rvHelper(layout Layout, kind, size uint32) ([]byte, error) {
	var a rv.Asm
	a.MovImm32(rv.T0, layout.HelperStateAddress)
	if !a.Sw(rv.A0, rv.T0, 0) {
		return nil, fmt.Errorf("qemu32: encode rv helper frame save")
	}
	a.MovImm32(rv.T1, layout.HelperHeaderAddress)
	for _, field := range []struct {
		offset int32
		value  uint32
	}{{0, protocolHelper}, {4, kind}, {8, size}} {
		a.MovImm32(rv.T2, field.value)
		if !a.Sw(rv.T2, rv.T1, field.offset) {
			return nil, fmt.Errorf("qemu32: encode rv helper header")
		}
	}
	a.MovImm32(rv.A0, 1)
	a.MovImm32(rv.A1, layout.HelperHeaderAddress)
	a.MovImm32(rv.A2, 12)
	a.MovImm32(rv.A7, 64)
	a.Ecall()
	a.MovImm32(rv.T0, layout.HelperStateAddress)
	if !a.Lw(rv.A1, rv.T0, 0) {
		return nil, fmt.Errorf("qemu32: encode rv helper frame reload")
	}
	a.MovImm32(rv.A0, 1)
	a.MovImm32(rv.A2, size)
	a.MovImm32(rv.A7, 64)
	a.Ecall()
	if kind == 3 {
		a.MovImm32(rv.A0, 0)
		a.MovImm32(rv.A1, layout.HelperHeaderAddress)
		a.MovImm32(rv.A2, 12)
		a.MovImm32(rv.A7, 63)
		a.Ecall()
		a.MovImm32(rv.T0, layout.HelperHeaderAddress)
		if !a.Lw(rv.A1, rv.T0, 0) || !a.Lw(rv.A2, rv.T0, 4) {
			return nil, fmt.Errorf("qemu32: encode rv helper memory request")
		}
		memoryReady := a.FarBcond(rv.A2, rv.Zero, rv.CondEQ, rv.T6)
		a.MovImm32(rv.A0, 1)
		a.MovImm32(rv.A7, 64)
		a.Ecall()
		if !a.PatchFarBranch(memoryReady, a.Len()) {
			return nil, fmt.Errorf("qemu32: patch rv helper memory-ready branch")
		}
	}
	a.MovImm32(rv.T0, layout.HelperStateAddress)
	if !a.Lw(rv.A1, rv.T0, 0) {
		return nil, fmt.Errorf("qemu32: encode rv helper read frame")
	}
	a.MovImm32(rv.A0, 0)
	a.MovImm32(rv.A2, size)
	a.MovImm32(rv.A7, 63)
	a.Ecall()
	if kind == 3 {
		a.MovImm32(rv.T0, layout.HelperHeaderAddress)
		if !a.Lw(rv.A1, rv.T0, 0) || !a.Lw(rv.A2, rv.T0, 8) {
			return nil, fmt.Errorf("qemu32: encode rv helper memory response")
		}
		memoryWritten := a.FarBcond(rv.A2, rv.Zero, rv.CondEQ, rv.T6)
		a.MovImm32(rv.A0, 0)
		a.MovImm32(rv.A7, 63)
		a.Ecall()
		if !a.PatchFarBranch(memoryWritten, a.Len()) {
			return nil, fmt.Errorf("qemu32: patch rv helper memory-written branch")
		}
	}
	a.Ret()
	return a.B, nil
}

func rvELF(code []byte) []byte {
	const codeOffset = 0x1000
	buf := bytes.NewBuffer(make([]byte, 0, codeOffset+len(code)))
	buf.Write([]byte{0x7f, 'E', 'L', 'F', 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	write := func(value any) { _ = binary.Write(buf, binary.LittleEndian, value) }
	write(uint16(2))
	write(uint16(243))
	write(uint32(1))
	write(CodeBase)
	write(uint32(52))
	write(uint32(0))
	write(uint32(0))
	write(uint16(52))
	write(uint16(32))
	write(uint16(1))
	write(uint16(0))
	write(uint16(0))
	write(uint16(0))
	write(uint32(1))
	write(uint32(codeOffset))
	write(CodeBase)
	write(CodeBase)
	write(uint32(len(code)))
	write(uint32(len(code)))
	write(uint32(7))
	write(uint32(0x1000))
	for buf.Len() < codeOffset {
		buf.WriteByte(0)
	}
	buf.Write(code)
	return buf.Bytes()
}

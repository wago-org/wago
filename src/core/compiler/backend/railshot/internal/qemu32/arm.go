package qemu32

import (
	"bytes"
	"encoding/binary"
	"fmt"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func ArmELF(image []byte) ([]byte, Layout, error) {
	layout := DataLayout(uint32(len(image)))
	main, err := armMain(layout)
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
		{ArmF32Helper &^ 1, 0, embedded32.F32FrameBytes},
		{ArmF64Helper &^ 1, 1, embedded32.F64FrameBytes},
		{ArmI64Helper &^ 1, 2, embedded32.I64FrameBytes},
		{ArmSIMDHelper &^ 1, 3, embedded32.SIMDFrameBytes},
	} {
		body, err := armHelper(layout, helper.kind, helper.size)
		if err != nil {
			return nil, Layout{}, err
		}
		offset := helper.address - CodeBase
		if uint64(offset)+uint64(len(body)) > uint64(len(code)) {
			return nil, Layout{}, fmt.Errorf("qemu32: arm helper %#x exceeds driver region", helper.address)
		}
		copy(code[offset:], body)
	}
	code = append(code, image...)
	end := layout.HelperStateAddress + 8
	for CodeBase+uint32(len(code)) < end {
		code = append(code, 0)
	}
	return armELF(code), layout, nil
}

func armMain(layout Layout) ([]byte, error) {
	var a a32.Asm
	must := func(ok bool, name string) error {
		if !ok {
			return fmt.Errorf("qemu32: encode arm %s", name)
		}
		return nil
	}
	emitRead := func(address uint32, countReg a32.Reg, count uint32) error {
		if err := must(a.MovImm32(a32.R0, 0), "read fd"); err != nil {
			return err
		}
		if err := must(a.MovImm32(a32.R1, address), "read address"); err != nil {
			return err
		}
		if countReg != a32.PC {
			if err := must(a.MovReg(a32.R2, countReg), "read variable count"); err != nil {
				return err
			}
		} else if err := must(a.MovImm32(a32.R2, count), "read count"); err != nil {
			return err
		}
		if err := must(a.MovImm32(a32.R7, 3), "read syscall"); err != nil {
			return err
		}
		a.Svc(0)
		return nil
	}
	emitWrite := func(address uint32, countReg a32.Reg, count uint32) error {
		if err := must(a.MovImm32(a32.R0, 1), "write fd"); err != nil {
			return err
		}
		if err := must(a.MovImm32(a32.R1, address), "write address"); err != nil {
			return err
		}
		if countReg != a32.PC {
			if err := must(a.MovReg(a32.R2, countReg), "write variable count"); err != nil {
				return err
			}
		} else if err := must(a.MovImm32(a32.R2, count), "write count"); err != nil {
			return err
		}
		if err := must(a.MovImm32(a32.R7, 4), "write syscall"); err != nil {
			return err
		}
		a.Svc(0)
		return nil
	}
	loop := a.Len()
	if err := emitRead(layout.RequestAddress, a32.PC, 20); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R3, layout.RequestAddress), "request address"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R4, a32.R3, 0), "request op"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, protocolCall), "call op"); err != nil {
		return nil, err
	}
	if err := must(a.Cmp(a32.R4, a32.R0), "call compare"); err != nil {
		return nil, err
	}
	callBranch := a.FarBcond(a32.CondEQ)
	if err := must(a.MovImm32(a32.R0, protocolStart), "start op"); err != nil {
		return nil, err
	}
	if err := must(a.Cmp(a32.R4, a32.R0), "start compare"); err != nil {
		return nil, err
	}
	startBranch := a.FarBcond(a32.CondEQ)
	if err := must(a.MovImm32(a32.R0, protocolRead), "read op"); err != nil {
		return nil, err
	}
	if err := must(a.Cmp(a32.R4, a32.R0), "read compare"); err != nil {
		return nil, err
	}
	readBranch := a.FarBcond(a32.CondEQ)
	if err := must(a.MovImm32(a32.R0, protocolWrite), "write op"); err != nil {
		return nil, err
	}
	if err := must(a.Cmp(a32.R4, a32.R0), "write compare"); err != nil {
		return nil, err
	}
	writeBranch := a.FarBcond(a32.CondEQ)
	if err := must(a.MovImm32(a32.R0, protocolExit), "exit op"); err != nil {
		return nil, err
	}
	if err := must(a.Cmp(a32.R4, a32.R0), "exit compare"); err != nil {
		return nil, err
	}
	exitBranch := a.FarBcond(a32.CondEQ)
	if err := must(a.MovImm32(a32.R0, 126), "bad op status"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R7, 1), "exit syscall"); err != nil {
		return nil, err
	}
	a.Svc(0)

	callTarget := a.Len()
	if err := must(a.MovImm32(a32.R3, layout.RequestAddress), "call request address"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R5, a32.R3, 12), "parameter count"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R6, a32.R3, 16), "result count"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, 0), "zero parameter count"); err != nil {
		return nil, err
	}
	if err := must(a.Cmp(a32.R5, a32.R0), "parameter compare"); err != nil {
		return nil, err
	}
	parametersReady := a.FarBcond(a32.CondEQ)
	if err := must(a.LslImm(a32.R2, a32.R5, 2), "parameter bytes"); err != nil {
		return nil, err
	}
	if err := emitRead(layout.RequestAddress+20, a32.R2, 0); err != nil {
		return nil, err
	}
	if !a.PatchFarBranch(parametersReady, a.Len()) {
		return nil, fmt.Errorf("qemu32: patch arm parameter branch")
	}
	if err := must(a.MovImm32(a32.R3, layout.RequestAddress), "call request reload"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R0, a32.R3, 8), "call context"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R3, a32.R0, embedded32.ContextTrapCellOffset), "qemu trap cell"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R2, 0), "qemu trap clear"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R2, a32.R3, 0), "qemu trap clear store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R2, 64*1024), "qemu stack budget"); err != nil {
		return nil, err
	}
	if err := must(a.Sub(a32.R2, a32.SP, a32.R2), "qemu stack limit"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R2, a32.R0, embedded32.ContextStackLimitOffset), "qemu stack limit store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R1, layout.CallAddress), "call abi address"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R1, embedded32.CallABIContextOffset), "call context store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, layout.RequestAddress+20), "parameters address"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R1, embedded32.CallABIParametersOffset), "parameters store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, layout.ResponseAddress+12), "results address"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R1, embedded32.CallABIResultsOffset), "results store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R3, layout.RequestAddress), "call entry request"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R12, a32.R3, 4), "call entry"); err != nil {
		return nil, err
	}
	if err := must(a.MovReg(a32.R0, a32.R1), "call argument"); err != nil {
		return nil, err
	}
	if err := must(a.Blx(a32.R12), "call entry invoke"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R3, layout.ResponseAddress), "response address"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R3, 4), "call code"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R1, protocolResult), "result tag"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R1, a32.R3, 0), "result tag store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R1, 0), "trap compare zero"); err != nil {
		return nil, err
	}
	if err := must(a.Cmp(a32.R0, a32.R1), "trap compare"); err != nil {
		return nil, err
	}
	trapped := a.FarBcond(a32.CondNE)
	if err := must(a.Str(a32.R6, a32.R3, 8), "result count store"); err != nil {
		return nil, err
	}
	resultCountReady := a.Branch()
	trappedTarget := a.Len()
	if err := must(a.Str(a32.R1, a32.R3, 8), "trapped result count"); err != nil {
		return nil, err
	}
	if err := must(a.MovReg(a32.R6, a32.R1), "trapped payload clear"); err != nil {
		return nil, err
	}
	resultCountTarget := a.Len()
	if !a.PatchFarBranch(trapped, trappedTarget) || !a.PatchBranch(resultCountReady, resultCountTarget) {
		return nil, fmt.Errorf("qemu32: patch arm result count")
	}
	if err := emitWrite(layout.ResponseAddress, a32.PC, 12); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, 0), "result zero"); err != nil {
		return nil, err
	}
	if err := must(a.Cmp(a32.R6, a32.R0), "result payload compare"); err != nil {
		return nil, err
	}
	noResults := a.FarBcond(a32.CondEQ)
	if err := must(a.LslImm(a32.R2, a32.R6, 2), "result bytes"); err != nil {
		return nil, err
	}
	if err := emitWrite(layout.ResponseAddress+12, a32.R2, 0); err != nil {
		return nil, err
	}
	if !a.PatchFarBranch(noResults, a.Len()) {
		return nil, fmt.Errorf("qemu32: patch arm no-results branch")
	}
	back := a.Branch()
	if !a.PatchBranch(back, loop) {
		return nil, fmt.Errorf("qemu32: patch arm call loop")
	}

	startTarget := a.Len()
	if err := must(a.MovImm32(a32.R3, layout.RequestAddress), "start request address"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R12, a32.R3, 4), "start entry"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R0, a32.R3, 8), "start context"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R3, a32.R0, embedded32.ContextTrapCellOffset), "start trap cell"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R2, 0), "start trap clear"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R2, a32.R3, 0), "start trap clear store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R2, 64*1024), "start stack budget"); err != nil {
		return nil, err
	}
	if err := must(a.Sub(a32.R2, a32.SP, a32.R2), "start stack limit"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R2, a32.R0, embedded32.ContextStackLimitOffset), "start stack limit store"); err != nil {
		return nil, err
	}
	if err := must(a.Blx(a32.R12), "start invoke"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R3, layout.ResponseAddress), "start response address"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R1, protocolResult), "start result tag"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R1, a32.R3, 0), "start result tag store"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R3, 4), "start code store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R1, 0), "start result zero"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R1, a32.R3, 8), "start result count"); err != nil {
		return nil, err
	}
	if err := emitWrite(layout.ResponseAddress, a32.PC, 12); err != nil {
		return nil, err
	}
	back = a.Branch()
	if !a.PatchBranch(back, loop) {
		return nil, fmt.Errorf("qemu32: patch arm start loop")
	}

	readTarget := a.Len()
	if err := must(a.MovImm32(a32.R3, layout.RequestAddress), "read request address"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R5, a32.R3, 4), "read source"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R6, a32.R3, 16), "read count"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R3, layout.ResponseAddress), "read response address"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, protocolResult), "read result tag"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R3, 0), "read result tag store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, 0), "read result code"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R3, 4), "read result code store"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R6, a32.R3, 8), "read result count store"); err != nil {
		return nil, err
	}
	if err := emitWrite(layout.ResponseAddress, a32.PC, 12); err != nil {
		return nil, err
	}
	if err := must(a.LslImm(a32.R2, a32.R6, 2), "read result bytes"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, 1), "read result fd"); err != nil {
		return nil, err
	}
	if err := must(a.MovReg(a32.R1, a32.R5), "read result source"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R7, 4), "read result write"); err != nil {
		return nil, err
	}
	a.Svc(0)
	back = a.Branch()
	if !a.PatchBranch(back, loop) {
		return nil, fmt.Errorf("qemu32: patch arm read loop")
	}

	writeTarget := a.Len()
	if err := must(a.MovImm32(a32.R3, layout.RequestAddress), "write request address"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R1, a32.R3, 4), "write destination"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R2, a32.R3, 16), "write byte count"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, 0), "write input fd"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R7, 3), "write input read"); err != nil {
		return nil, err
	}
	a.Svc(0)
	if err := must(a.MovImm32(a32.R3, layout.ResponseAddress), "write response address"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, protocolResult), "write result tag"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R3, 0), "write result tag store"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, 0), "write result zero"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R3, 4), "write result code store"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R3, 8), "write result count store"); err != nil {
		return nil, err
	}
	if err := emitWrite(layout.ResponseAddress, a32.PC, 12); err != nil {
		return nil, err
	}
	back = a.Branch()
	if !a.PatchBranch(back, loop) {
		return nil, fmt.Errorf("qemu32: patch arm write loop")
	}

	exitTarget := a.Len()
	if err := must(a.MovImm32(a32.R0, 0), "exit status"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R7, 1), "exit syscall"); err != nil {
		return nil, err
	}
	a.Svc(0)
	if !a.PatchFarBranch(callBranch, callTarget) || !a.PatchFarBranch(startBranch, startTarget) || !a.PatchFarBranch(readBranch, readTarget) || !a.PatchFarBranch(writeBranch, writeTarget) || !a.PatchFarBranch(exitBranch, exitTarget) {
		return nil, fmt.Errorf("qemu32: patch arm dispatch")
	}
	return a.B, nil
}

func armHelper(layout Layout, kind, size uint32) ([]byte, error) {
	var a a32.Asm
	must := func(ok bool, name string) error {
		if !ok {
			return fmt.Errorf("qemu32: encode arm helper %s", name)
		}
		return nil
	}
	if err := must(a.MovImm32(a32.R3, layout.HelperStateAddress), "state"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R0, a32.R3, 0), "frame save"); err != nil {
		return nil, err
	}
	if err := must(a.Str(a32.R7, a32.R3, 4), "r7 save"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R1, layout.HelperHeaderAddress), "header"); err != nil {
		return nil, err
	}
	for _, field := range []struct {
		offset uint16
		value  uint32
	}{{0, protocolHelper}, {4, kind}, {8, size}} {
		if err := must(a.MovImm32(a32.R2, field.value), "header value"); err != nil {
			return nil, err
		}
		if err := must(a.Str(a32.R2, a32.R1, field.offset), "header store"); err != nil {
			return nil, err
		}
	}
	if err := must(a.MovImm32(a32.R0, 1), "header fd"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R2, 12), "header bytes"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R7, 4), "header write"); err != nil {
		return nil, err
	}
	a.Svc(0)
	if err := must(a.MovImm32(a32.R3, layout.HelperStateAddress), "state reload"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R1, a32.R3, 0), "frame reload"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, 1), "frame write fd"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R2, size), "frame bytes"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R7, 4), "frame write"); err != nil {
		return nil, err
	}
	a.Svc(0)
	if kind == 3 {
		if err := must(a.MovImm32(a32.R0, 0), "memory request fd"); err != nil {
			return nil, err
		}
		if err := must(a.MovImm32(a32.R1, layout.HelperHeaderAddress), "memory request address"); err != nil {
			return nil, err
		}
		if err := must(a.MovImm32(a32.R2, 12), "memory request bytes"); err != nil {
			return nil, err
		}
		if err := must(a.MovImm32(a32.R7, 3), "memory request read"); err != nil {
			return nil, err
		}
		a.Svc(0)
		if err := must(a.MovImm32(a32.R3, layout.HelperHeaderAddress), "memory request reload"); err != nil {
			return nil, err
		}
		if err := must(a.Ldr(a32.R1, a32.R3, 0), "memory source"); err != nil {
			return nil, err
		}
		if err := must(a.Ldr(a32.R2, a32.R3, 4), "memory read width"); err != nil {
			return nil, err
		}
		if err := must(a.MovImm32(a32.R0, 0), "memory width zero"); err != nil {
			return nil, err
		}
		if err := must(a.Cmp(a32.R2, a32.R0), "memory width compare"); err != nil {
			return nil, err
		}
		memoryReady := a.FarBcond(a32.CondEQ)
		if err := must(a.MovImm32(a32.R0, 1), "memory write fd"); err != nil {
			return nil, err
		}
		if err := must(a.MovImm32(a32.R7, 4), "memory write"); err != nil {
			return nil, err
		}
		a.Svc(0)
		if !a.PatchFarBranch(memoryReady, a.Len()) {
			return nil, fmt.Errorf("qemu32: patch arm memory-ready branch")
		}
	}
	if err := must(a.MovImm32(a32.R3, layout.HelperStateAddress), "read state"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R1, a32.R3, 0), "read frame"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R0, 0), "frame read fd"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R2, size), "frame read bytes"); err != nil {
		return nil, err
	}
	if err := must(a.MovImm32(a32.R7, 3), "frame read"); err != nil {
		return nil, err
	}
	a.Svc(0)
	if kind == 3 {
		if err := must(a.MovImm32(a32.R3, layout.HelperHeaderAddress), "memory response"); err != nil {
			return nil, err
		}
		if err := must(a.Ldr(a32.R1, a32.R3, 0), "memory destination"); err != nil {
			return nil, err
		}
		if err := must(a.Ldr(a32.R2, a32.R3, 8), "memory write width"); err != nil {
			return nil, err
		}
		if err := must(a.MovImm32(a32.R0, 0), "memory write zero"); err != nil {
			return nil, err
		}
		if err := must(a.Cmp(a32.R2, a32.R0), "memory write compare"); err != nil {
			return nil, err
		}
		memoryWritten := a.FarBcond(a32.CondEQ)
		if err := must(a.MovImm32(a32.R0, 0), "memory read fd"); err != nil {
			return nil, err
		}
		if err := must(a.MovImm32(a32.R7, 3), "memory read"); err != nil {
			return nil, err
		}
		a.Svc(0)
		if !a.PatchFarBranch(memoryWritten, a.Len()) {
			return nil, fmt.Errorf("qemu32: patch arm memory-written branch")
		}
	}
	if err := must(a.MovImm32(a32.R3, layout.HelperStateAddress), "restore state"); err != nil {
		return nil, err
	}
	if err := must(a.Ldr(a32.R7, a32.R3, 4), "r7 restore"); err != nil {
		return nil, err
	}
	a.Ret()
	a.Align4()
	return a.B, nil
}

func armELF(code []byte) []byte {
	const codeOffset = 0x1000
	buf := bytes.NewBuffer(make([]byte, 0, codeOffset+len(code)))
	buf.Write([]byte{0x7f, 'E', 'L', 'F', 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	write := func(value any) { _ = binary.Write(buf, binary.LittleEndian, value) }
	write(uint16(2))
	write(uint16(40))
	write(uint32(1))
	write(CodeBase | 1)
	write(uint32(52))
	write(uint32(0))
	write(uint32(0x05000200))
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

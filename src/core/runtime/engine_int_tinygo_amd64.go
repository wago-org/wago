//go:build linux && amd64 && tinygo

package runtime

import (
	"encoding/binary"
	"unsafe"
)

// TinyGo cannot assemble engine_int_amd64.s. Its indirect-call ABI passes the
// five explicit uintptr arguments in RDI, RSI, RDX, RCX, and R8, followed by the
// func-value context in R9. The generated thunk rearranges those registers into
// Wago's direct integer ABI and performs the same foreign-stack transition as
// the standard Go assembly entry.
var tinygoIntThunkTemplate = []byte{
	0x49, 0xba, 0, 0, 0, 0, 0, 0, 0, 0, // movabs stackTop, r10
	0x49, 0x83, 0xea, 0x40, // sub $64, r10
	0x49, 0x89, 0x22, // mov rsp, 0(r10)
	0x49, 0x89, 0x6a, 0x08, // mov rbp, 8(r10)
	0x49, 0x89, 0x5a, 0x10, // mov rbx, 16(r10)
	0x4d, 0x89, 0x62, 0x18, // mov r12, 24(r10)
	0x4d, 0x89, 0x6a, 0x20, // mov r13, 32(r10)
	0x4d, 0x89, 0x72, 0x28, // mov r14, 40(r10)
	0x4d, 0x89, 0x7a, 0x30, // mov r15, 48(r10)
	0x48, 0x89, 0xfb, // mov rdi, rbx (linMem)
	0x49, 0x8d, 0x7a, 0xf8, // lea -8(r10), rdi
	0x48, 0x89, 0x7b, 0xe8, // mov rdi, -24(rbx)
	0x49, 0x89, 0xcb, // mov rcx, r11 (preserve a2)
	0x48, 0x89, 0xf0, // mov rsi, rax (a0)
	0x48, 0x89, 0xd1, // mov rdx, rcx (a1)
	0x4c, 0x89, 0xda, // mov r11, rdx (a2)
	0x4c, 0x89, 0xd4, // mov r10, rsp
	0x31, 0xed, // xor ebp, ebp
	0x41, 0xff, 0xd1, // call r9 (func-value context = code)
	0x48, 0x8b, 0x6c, 0x24, 0x08, // mov 8(rsp), rbp
	0x48, 0x8b, 0x5c, 0x24, 0x10, // mov 16(rsp), rbx
	0x4c, 0x8b, 0x64, 0x24, 0x18, // mov 24(rsp), r12
	0x4c, 0x8b, 0x6c, 0x24, 0x20, // mov 32(rsp), r13
	0x4c, 0x8b, 0x74, 0x24, 0x28, // mov 40(rsp), r14
	0x4c, 0x8b, 0x7c, 0x24, 0x30, // mov 48(rsp), r15
	0x48, 0x8b, 0x24, 0x24, // mov 0(rsp), rsp
	0xc3, // ret
}

const tinygoIntThunkStackTopOff = 2

func preparedIntThunkFor(e *Engine) (uintptr, error) {
	if e.preparedInt.entry != 0 {
		return e.preparedInt.entry, nil
	}
	code := make([]byte, len(tinygoIntThunkTemplate))
	copy(code, tinygoIntThunkTemplate)
	binary.LittleEndian.PutUint64(code[tinygoIntThunkStackTopOff:], uint64(e.stackTop))
	mem, err := mmapExec(code)
	if err != nil {
		return 0, err
	}
	e.preparedInt.mem = mem
	e.preparedInt.entry = uintptr(unsafe.Pointer(&mem[0]))
	return e.preparedInt.entry, nil
}

func (e *Engine) EnterPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64) (uint64, error) {
	entry, err := preparedIntThunkFor(e)
	if err != nil {
		return 0, err
	}
	fv := funcValue{context: code, fnptr: entry}
	call := *(*func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr)(unsafe.Pointer(&fv))
	return uint64(call(linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3))), nil
}

func PreparedIntTrapCode(trap []byte) TrapCode {
	if len(trap) < 4 {
		return TrapNone
	}
	return TrapCode(loadTrap(trap))
}

func ConsumePreparedIntTrap(trap []byte) error {
	tc := PreparedIntTrapCode(trap)
	if tc == TrapNone {
		return nil
	}
	storeTrap(trap, 0)
	return trapErrorFromBuffer(tc, trap)
}

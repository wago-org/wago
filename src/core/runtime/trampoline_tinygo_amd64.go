//go:build linux && amd64 && tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

// TinyGo cannot assemble Go's Plan9 .s files, so the assembly trampoline used by
// the standard toolchain (trampoline_amd64.s) is unavailable here. Instead we
// build the trampoline as machine code at run time — the same approach the
// engine already uses for its native code — and invoke it through an unsafe
// func-value cast. No cgo is involved: a cgo call would impose a boundary
// transition on every wasm invocation, which is exactly the latency this engine
// is built to avoid.
//
// The trick relies on how TinyGo lowers an indirect call through a func value to
// the System V C ABI. For a call `f(a, b, c, d)`, TinyGo passes the four
// arguments in RDI, RSI, RDX, RCX and the func value's context word in the next
// integer register, R8. That maps onto WARP's WasmWrapper ABI for free: the four
// arguments already arrive in exactly the registers the native code expects
// (serArgs->RDI, linMem->RSI, trap->RDX, results->RCX), and we smuggle the
// native code pointer through the context word so it arrives in R8. The
// generated thunk switches RSP to the engine's foreign stack (its top baked in
// as an immediate), `call`s R8, then restores the Go context — mirroring
// trampoline_amd64.s exactly.

// thunkTemplate is the foreign-stack-switch trampoline as position-independent
// machine code. The 8 bytes at thunkStackTopOff are a placeholder for the
// foreign-stack top, patched per engine stack.
//
// On entry (System V): RDI=serArgs, RSI=linMem, RDX=trap, RCX=results, R8=code.
//
//	movabs $imm64, %r10        ; foreign-stack top (patched at thunkStackTopOff)
//	sub    $0x40, %r10         ; reserve a 64-byte Go-context save area
//	mov    %rsp, (%r10)        ; save goroutine SP + callee-saved registers
//	mov    %rbp, 0x8(%r10)
//	mov    %rbx, 0x10(%r10)
//	mov    %r12, 0x18(%r10)
//	mov    %r13, 0x20(%r10)
//	mov    %r14, 0x28(%r10)
//	mov    %r15, 0x30(%r10)
//	mov    %r10, %rsp          ; switch to the foreign stack
//	xor    %ebp, %ebp          ; zero RBP so any unwinder stops here
//	lea    -0x8(%r10), %rax    ; handler-jump trap re-entry SP (post-call push)
//	mov    %rax, -0x18(%rsi)   ; store at [linMem - offTrapStackReentry]
//	call   *%r8                ; run WARP code (args already in RDI..RCX)
//	mov    0x8(%rsp), %rbp     ; restore the Go context (SP == save area)
//	mov    0x10(%rsp), %rbx
//	mov    0x18(%rsp), %r12
//	mov    0x20(%rsp), %r13
//	mov    0x28(%rsp), %r14
//	mov    0x30(%rsp), %r15
//	mov    (%rsp), %rsp        ; back on the goroutine stack
//	ret
var thunkTemplate = []byte{
	0x49, 0xba, 0, 0, 0, 0, 0, 0, 0, 0, // movabs imm64 -> r10 (imm at offset 2)
	0x49, 0x83, 0xea, 0x40, // sub $0x40, %r10
	0x49, 0x89, 0x22, // mov %rsp, (%r10)
	0x49, 0x89, 0x6a, 0x08, // mov %rbp, 0x8(%r10)
	0x49, 0x89, 0x5a, 0x10, // mov %rbx, 0x10(%r10)
	0x4d, 0x89, 0x62, 0x18, // mov %r12, 0x18(%r10)
	0x4d, 0x89, 0x6a, 0x20, // mov %r13, 0x20(%r10)
	0x4d, 0x89, 0x72, 0x28, // mov %r14, 0x28(%r10)
	0x4d, 0x89, 0x7a, 0x30, // mov %r15, 0x30(%r10)
	0x4c, 0x89, 0xd4, // mov %r10, %rsp
	0x31, 0xed, // xor %ebp, %ebp
	0x49, 0x8d, 0x42, 0xf8, // lea -0x8(%r10), %rax
	0x48, 0x89, 0x46, 0xe8, // mov %rax, -0x18(%rsi)  (offTrapStackReentry = 24)
	0x41, 0xff, 0xd0, // call *%r8
	0x48, 0x8b, 0x6c, 0x24, 0x08, // mov 0x8(%rsp), %rbp
	0x48, 0x8b, 0x5c, 0x24, 0x10, // mov 0x10(%rsp), %rbx
	0x4c, 0x8b, 0x64, 0x24, 0x18, // mov 0x18(%rsp), %r12
	0x4c, 0x8b, 0x6c, 0x24, 0x20, // mov 0x20(%rsp), %r13
	0x4c, 0x8b, 0x74, 0x24, 0x28, // mov 0x28(%rsp), %r14
	0x4c, 0x8b, 0x7c, 0x24, 0x30, // mov 0x30(%rsp), %r15
	0x48, 0x8b, 0x24, 0x24, // mov (%rsp), %rsp
	0xc3, // ret
}

const thunkStackTopOff = 2

var (
	tinygoPreparedIntEntryOffset = uintptr(len(thunkTemplate))
	tinygoMmapExec               = mmapExec
)

// funcValue mirrors TinyGo's in-memory representation of a func value:
// {context, fnptr}. Overlaying it lets us synthesize a callable that jumps to
// arbitrary machine code with a chosen context word.
type funcValue struct {
	context uintptr
	fnptr   uintptr
}

// initNativeEntry creates the wrapper and prepared-integer entry thunks in one
// Engine-owned executable mapping. Engine.Close releases the mapping with the
// 4 MiB foreign stack, so historical stack addresses retain no executable page.
func (e *Engine) initNativeEntry() error {
	if e.preparedInt.mem != nil {
		return nil
	}
	stackTop := e.stackTop
	code := make([]byte, len(thunkTemplate)+len(tinygoIntThunkTemplate))
	copy(code, thunkTemplate)
	copy(code[len(thunkTemplate):], tinygoIntThunkTemplate)
	binary.LittleEndian.PutUint64(code[thunkStackTopOff:], uint64(stackTop))
	binary.LittleEndian.PutUint64(code[len(thunkTemplate)+tinygoIntThunkStackTopOff:], uint64(stackTop))
	mem, err := tinygoMmapExec(code)
	if err != nil {
		return fmt.Errorf("tinygo entry trampoline: %w", err)
	}
	e.preparedInt.mem = mem
	e.preparedInt.stackTop = stackTop
	e.stackTop = uintptr(unsafe.Pointer(&mem[0]))
	return nil
}

// enterNative runs WARP code at code on the Engine-specific entry trampoline.
func enterNative(code, serArgs, linMem, trap, results, entry uintptr) {
	if entry == 0 {
		markTinyGoTrampolineFailure(trap)
		return
	}
	fv := funcValue{context: code, fnptr: entry}
	call := *(*func(a, b, c, d uintptr))(unsafe.Pointer(&fv))
	call(serArgs, linMem, trap, results)
}

// resumeThunkTemplate is the TinyGo counterpart of resume_amd64.s: a position-
// independent (and foreign-stack-top-independent, since that arrives in a
// register) resume trampoline. Entered via a func-value cast with RDI=ctrl,
// RSI=foreignStackTop. Unlike the standard toolchain's enterNative, the TinyGo
// enterNative thunk's epilogue is `mov (%rsp),%rsp; ret` (no POPQ BP), so this
// stashes the goroutine SP pointing directly at the return address. Assembled
// from resumethunk.s (`as` + objdump).
var resumeThunkTemplate = []byte{
	0x48, 0x83, 0xee, 0x40, // sub $0x40, %rsi          ; save-area base
	0x48, 0x89, 0x26, //       mov %rsp, (%rsi)         ; goroutine SP (-> return address)
	0x48, 0x89, 0x6e, 0x08, // mov %rbp, 0x8(%rsi)
	0x48, 0x89, 0x5e, 0x10, // mov %rbx, 0x10(%rsi)
	0x4c, 0x89, 0x66, 0x18, // mov %r12, 0x18(%rsi)
	0x4c, 0x89, 0x6e, 0x20, // mov %r13, 0x20(%rsi)
	0x4c, 0x89, 0x76, 0x28, // mov %r14, 0x28(%rsi)
	0x4c, 0x89, 0x7e, 0x30, // mov %r15, 0x30(%rsi)
	0x48, 0x8b, 0x5f, 0x08, // mov 0x8(%rdi), %rbx      ; restore wasm state from ctrl
	0x48, 0x8b, 0x6f, 0x10, // mov 0x10(%rdi), %rbp
	0x4c, 0x8b, 0x67, 0x18, // mov 0x18(%rdi), %r12
	0x4c, 0x8b, 0x6f, 0x20, // mov 0x20(%rdi), %r13
	0x4c, 0x8b, 0x77, 0x28, // mov 0x28(%rdi), %r14
	0x4c, 0x8b, 0x7f, 0x30, // mov 0x30(%rdi), %r15
	0x48, 0x8b, 0x27, //       mov (%rdi), %rsp         ; hcSavedRSP -> deep wasm stack
	0xc3, //                   ret
}

var (
	resumeThunkMu    sync.Mutex
	resumeThunkEntry uintptr
)

func resumeThunkPtr() uintptr {
	if entry := atomic.LoadUintptr(&resumeThunkEntry); entry != 0 {
		return entry
	}
	resumeThunkMu.Lock()
	defer resumeThunkMu.Unlock()
	if entry := atomic.LoadUintptr(&resumeThunkEntry); entry != 0 {
		return entry
	}
	code := make([]byte, len(resumeThunkTemplate))
	copy(code, resumeThunkTemplate)
	mem, err := tinygoMmapExec(code)
	if err != nil {
		return 0
	}
	entry := uintptr(unsafe.Pointer(&mem[0])) // retained for the process
	atomic.StoreUintptr(&resumeThunkEntry, entry)
	return entry
}

// resumeNative resumes native code parked at a host call (see hostcall_amd64.go).
// It mirrors resume_amd64.s. The thunk ignores the func-value context word.
func resumeNative(ctrl, foreignStackTop uintptr) {
	entry := resumeThunkPtr()
	if entry == 0 {
		markTinyGoResumeFailure(ctrl)
		return
	}
	fv := funcValue{context: 0, fnptr: entry}
	call := *(*func(a, b uintptr))(unsafe.Pointer(&fv))
	call(ctrl, foreignStackTop)
}

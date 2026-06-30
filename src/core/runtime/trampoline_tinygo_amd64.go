//go:build linux && amd64 && tinygo

package runtime

import (
	"encoding/binary"
	"sync"
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

// funcValue mirrors TinyGo's in-memory representation of a func value:
// {context, fnptr}. Overlaying it lets us synthesize a callable that jumps to
// arbitrary machine code with a chosen context word.
type funcValue struct {
	context uintptr
	fnptr   uintptr
}

var (
	thunkMu    sync.Mutex
	thunkCache = map[uintptr]uintptr{} // foreign-stack top -> thunk entry point
)

// thunkFor returns the entry point of a foreign-stack-switch trampoline
// specialized for foreignStackTop, generating and caching it on first use. The
// mapping is kept for the life of the process (one executable page per distinct
// engine stack, of which there are typically very few).
func thunkFor(foreignStackTop uintptr) uintptr {
	thunkMu.Lock()
	defer thunkMu.Unlock()
	if entry, ok := thunkCache[foreignStackTop]; ok {
		return entry
	}
	code := make([]byte, len(thunkTemplate))
	copy(code, thunkTemplate)
	binary.LittleEndian.PutUint64(code[thunkStackTopOff:], uint64(foreignStackTop))
	mem, err := mmapExec(code)
	if err != nil {
		panic("wago: cannot map tinygo trampoline: " + err.Error())
	}
	entry := uintptr(unsafe.Pointer(&mem[0]))
	thunkCache[foreignStackTop] = entry
	return entry
}

// enterNative runs WARP code at code on the engine's foreign stack following the
// WasmWrapper ABI, then returns to Go. See the file comment for how the
// arguments reach the native code with no assembly and no cgo.
func enterNative(code, serArgs, linMem, trap, results, foreignStackTop uintptr) {
	fv := funcValue{context: code, fnptr: thunkFor(foreignStackTop)}
	call := *(*func(a, b, c, d uintptr))(unsafe.Pointer(&fv))
	call(serArgs, linMem, trap, results)
}

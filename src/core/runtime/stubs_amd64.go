//go:build linux && amd64

package runtime

import "encoding/binary"

// Hand-written amd64 machine-code stubs that honor WARP's WasmWrapper ABI on
// entry: RDI=serArgs, RSI=linMem, RDX=trap, RCX=results. Each ends with RET.
// They implement only the contract-visible behavior (no full prologue/spill) —
// enough to prove the Go host side before the real backend exists. serArgs and
// results are 8-byte slots (WARP's WasmValue layout).

// stubAdd1: results[0] = serArgs[0]+1 (i32); trap = NONE.
//
//	8B 07              mov   eax, [rdi]      ; serArgs slot0 (i32 arg)
//	83 C0 01           add   eax, 1
//	89 01              mov   [rcx], eax      ; results slot0
//	C7 02 00 00 00 00  mov   dword [rdx], 0  ; *trap = NONE
//	C3                 ret
var stubAdd1 = []byte{
	0x8B, 0x07,
	0x83, 0xC0, 0x01,
	0x89, 0x01,
	0xC7, 0x02, 0x00, 0x00, 0x00, 0x00,
	0xC3,
}

// stubMemStore: linMem[serArgs[0]] = serArgs[1] (i32); trap = NONE.
//
//	48 8B 07           mov   rax, [rdi]      ; offset (slot0)
//	8B 4F 08           mov   ecx, [rdi+8]    ; value  (slot1)
//	89 0C 06           mov   [rsi+rax], ecx
//	C7 02 00 00 00 00  mov   dword [rdx], 0
//	C3                 ret
var stubMemStore = []byte{
	0x48, 0x8B, 0x07,
	0x8B, 0x4F, 0x08,
	0x89, 0x0C, 0x06,
	0xC7, 0x02, 0x00, 0x00, 0x00, 0x00,
	0xC3,
}

// stubMemLoad: results[0] = linMem[serArgs[0]] (i32); trap = NONE.
// NB: RCX holds the results pointer on entry, so the loaded value must go in a
// different scratch register (r8d) — using ecx would clobber results.
//
//	48 8B 07           mov   rax, [rdi]      ; offset (slot0)
//	44 8B 04 06        mov   r8d, [rsi+rax]
//	44 89 01           mov   [rcx], r8d      ; results slot0
//	C7 02 00 00 00 00  mov   dword [rdx], 0
//	C3                 ret
var stubMemLoad = []byte{
	0x48, 0x8B, 0x07,
	0x44, 0x8B, 0x04, 0x06,
	0x44, 0x89, 0x01,
	0xC7, 0x02, 0x00, 0x00, 0x00, 0x00,
	0xC3,
}

// stubTrap returns with *trap set to code (results untouched).
//
//	C7 02 <code u32>   mov   dword [rdx], code
//	C3                 ret
func stubTrap(code TrapCode) []byte {
	b := []byte{0xC7, 0x02, 0, 0, 0, 0, 0xC3}
	binary.LittleEndian.PutUint32(b[2:], uint32(code))
	return b
}

// stubLoop runs a bounded counted loop (serArgs[0] iterations), then writes the
// sentinel 0x5A5A5A5A into linMem[0] and results[0]; trap = NONE. Used by the
// GC/preemption stress test to keep native code running while GC churns.
//
//	8B 07                  mov   eax, [rdi]            ; iterations
//
// loop:
//
//	85 C0                  test  eax, eax
//	74 05                  jz    done
//	83 E8 01               sub   eax, 1
//	EB F7                  jmp   loop
//
// done:
//
//	C7 06 5A 5A 5A 5A      mov   dword [rsi], 0x5A5A5A5A   ; linMem[0]
//	C7 01 5A 5A 5A 5A      mov   dword [rcx], 0x5A5A5A5A   ; results[0]
//	C7 02 00 00 00 00      mov   dword [rdx], 0            ; *trap = NONE
//	C3                     ret
var stubLoop = []byte{
	0x8B, 0x07,
	0x85, 0xC0,
	0x74, 0x05,
	0x83, 0xE8, 0x01,
	0xEB, 0xF7,
	0xC7, 0x06, 0x5A, 0x5A, 0x5A, 0x5A,
	0xC7, 0x01, 0x5A, 0x5A, 0x5A, 0x5A,
	0xC7, 0x02, 0x00, 0x00, 0x00, 0x00,
	0xC3,
}

const loopSentinel = 0x5A5A5A5A

// Control-block layout for the V2 host-import re-entry protocol (off-heap; its
// address is stored in basedata at [linMem-offCustomCtx], so native code reaches
// it as ctx). All fields are u32.
const (
	ctrlState = 0  // 0 = fresh, 1 = host call requested/in-flight
	ctrlArg   = 8  // native -> Go: argument for the host function
	ctrlRet   = 12 // Go -> native: host function result
	ctrlSize  = 16
)

// hostCallPending is the sentinel written to *trap by a stub that wants the Go
// side to run a host import and re-enter. It is outside the TrapCode range.
const hostCallPending = 0x10000

// stubHostCall demonstrates a V2 host-import call via the safe re-entry protocol
// (never calls Go from the foreign stack). It is a small state machine reached
// possibly twice per logical wasm call:
//
//	ctx = [linMem - 40]              ; control block pointer (WARP customCtxOffset)
//	state 0: stash serArgs[0] as the host arg, set state=1, *trap=HOSTCALL_PENDING, ret
//	state 1: results[0] = hostResult + 1 (proves native runs after the call), *trap=NONE, ret
//
// Bytes assembled with `as` and verified via objdump (see job tmp/hoststub.s).
var stubHostCall = []byte{
	0x4c, 0x8b, 0x4e, 0xd8, // mov r9, [rsi-0x28]      ; r9 = ctx (control block)
	0x41, 0x8b, 0x01, //       mov eax, [r9]           ; eax = state
	0x85, 0xc0, //             test eax, eax
	0x75, 0x14, //             jne resume
	0x8b, 0x07, //             mov eax, [rdi]          ; arg = serArgs[0]
	0x41, 0x89, 0x41, 0x08, // mov [r9+8], eax         ; cb.callArg = arg
	0x41, 0xc7, 0x01, 0x01, 0x00, 0x00, 0x00, // mov dword [r9], 1   ; cb.state = 1
	0xc7, 0x02, 0x00, 0x00, 0x01, 0x00, //       mov dword [rdx], 0x10000 ; *trap = pending
	0xc3, //                   ret
	// resume:
	0x41, 0x8b, 0x41, 0x0c, // mov eax, [r9+0xc]       ; eax = cb.callRet
	0x83, 0xc0, 0x01, //       add eax, 1              ; +1 (post-call native work)
	0x89, 0x01, //             mov [rcx], eax          ; results[0]
	0x41, 0xc7, 0x01, 0x00, 0x00, 0x00, 0x00, // mov dword [r9], 0   ; cb.state = 0
	0xc7, 0x02, 0x00, 0x00, 0x00, 0x00, //       mov dword [rdx], 0  ; *trap = NONE
	0xc3, //                   ret
}

// stubLoopHeartbeat is like stubLoop but writes the live counter into linMem[0]
// on every iteration, so another goroutine can observe that native code is
// actively running (heartbeat != 0) and time a GC requested mid-run.
//
//	8B 07                  mov   eax, [rdi]            ; iterations
//
// loop:
//
//	89 06                  mov   [rsi], eax            ; linMem[0] = counter (heartbeat)
//	85 C0                  test  eax, eax
//	74 05                  jz    done
//	83 E8 01               sub   eax, 1
//	EB F5                  jmp   loop
//
// done:
//
//	C7 01 5A 5A 5A 5A      mov   dword [rcx], 0x5A5A5A5A
//	C7 02 00 00 00 00      mov   dword [rdx], 0
//	C3                     ret
var stubLoopHeartbeat = []byte{
	0x8B, 0x07,
	0x89, 0x06,
	0x85, 0xC0,
	0x74, 0x05,
	0x83, 0xE8, 0x01,
	0xEB, 0xF5,
	0xC7, 0x01, 0x5A, 0x5A, 0x5A, 0x5A,
	0xC7, 0x02, 0x00, 0x00, 0x00, 0x00,
	0xC3,
}

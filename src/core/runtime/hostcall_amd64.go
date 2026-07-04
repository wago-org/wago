//go:build linux && amd64

package runtime

import "sync"

// Synchronous host-import re-entry protocol (P8.1). A returning host import
// cannot use the async log-and-replay path (the host must run *during* the wasm
// call to hand a value back). Instead native code parks itself: it calls the
// shared hostCallStub, which saves the live wasm register state into an off-heap
// control frame and unwinds to Go through the existing trap re-entry SP; Go runs
// the host function on the goroutine stack and writes the results; resumeNative
// restores the saved register state and returns to the instruction after the
// call. See docs/host-import-results-plan.md §2.

// maxHostArity bounds the params/results a single host import may carry through
// the control frame. WASI preview 1 tops out well below this. Changing it shifts
// hcResults, so the hand-assembled stubs that hard-code that offset must move
// too.
const maxHostArity = 8

// Control-frame field offsets (bytes). Off-heap; the frame's address is
// installed in basedata at offCustomCtx, so native code reaches it as
// [linMem-offCustomCtx]. hostCallStub writes the hcSaved* slots; Go reads
// hcImportIdx/hcNArgs/hcArgs and writes hcResults; resumeNative reads hcSaved*.
const (
	hcSavedRSP    = 0                       // u64: wasm RSP at the call site (points at the resume address)
	hcSavedRBX    = 8                       // u64
	hcSavedRBP    = 16                      // u64
	hcSavedR12    = 24                      // u64
	hcSavedR13    = 32                      // u64
	hcSavedR14    = 40                      // u64
	hcSavedR15    = 48                      // u64
	hcTrampoline  = 56                      // u64: hostCallStub address (per-instance constant, published by CallWithHost)
	hcImportIdx   = 64                      // u32: native -> Go, which import
	hcNArgs       = 68                      // u32: native -> Go, number of marshaled args
	hcArgs        = 72                      // [maxHostArity]u64: native -> Go
	hcResults     = hcArgs + maxHostArity*8 // [maxHostArity]u64: Go -> native (== 136 for maxHostArity=8)
	ctrlFrameSize = hcResults + maxHostArity*8
)

// hostCallPending is written to the trap cell by hostCallStub to ask the Go exec
// loop to run a host import and resume. It is outside the TrapCode range.
const hostCallPending = 0x10000

// HostCall runs the bound host import importIdx with the argument slots args and
// writes its results into results (results has len maxHostArity; only the leading
// slots the import's signature defines are meaningful). It runs on the goroutine
// stack in normal Go context, so arbitrary host code — allocation, even a nested
// wasm invocation — is safe (no foreign-stack/morestack hazard).
type HostCall func(importIdx uint32, args, results []uint64)

// hostCallStub is shared, position-independent machine code entered by native
// code via `call [ctrl+hcTrampoline]` (rbx = linMem, rsp -> the wasm resume
// address). It saves the wasm callee-saved registers + RSP into the control
// frame at [rbx-offCustomCtx], writes hostCallPending into the trap cell at
// [rbx-TrapCellPtrOffset], then unwinds to Go via the trap re-entry SP at
// [rbx-offTrapStackReentry] exactly like the trap path. Assembled from
// hoststub.s (`as` + objdump); the disassembly offsets are -0x28 (offCustomCtx
// 40), -0x68 (TrapCellPtrOffset 104), -0x18 (offTrapStackReentry 24).
var hostCallStub = []byte{
	0x4c, 0x8b, 0x4b, 0xd8, // mov  -0x28(%rbx),%r9      ; r9 = ctrl
	0x49, 0x89, 0x21, //       mov  %rsp,(%r9)           ; hcSavedRSP
	0x49, 0x89, 0x59, 0x08, // mov  %rbx,0x8(%r9)        ; hcSavedRBX
	0x49, 0x89, 0x69, 0x10, // mov  %rbp,0x10(%r9)       ; hcSavedRBP
	0x4d, 0x89, 0x61, 0x18, // mov  %r12,0x18(%r9)
	0x4d, 0x89, 0x69, 0x20, // mov  %r13,0x20(%r9)
	0x4d, 0x89, 0x71, 0x28, // mov  %r14,0x28(%r9)
	0x4d, 0x89, 0x79, 0x30, // mov  %r15,0x30(%r9)
	0x4c, 0x8b, 0x43, 0x98, // mov  -0x68(%rbx),%r8      ; r8 = trap cell ptr
	0x41, 0xc7, 0x00, 0x00, 0x00, 0x01, 0x00, // movl $0x10000,(%r8)  ; hostCallPending
	0x48, 0x8b, 0x63, 0xe8, // mov  -0x18(%rbx),%rsp     ; trap re-entry SP
	0xc3, //                   ret                       ; -> shared enterNative epilogue
}

// hostCallStub is mapped once for the process: it is position-independent and
// identical for every engine, so a single executable page (never unmapped)
// serves all of them.
var (
	hostStubOnce sync.Once
	hostStubPtr  uintptr
	hostStubErr  error
)

func hostCallStubPtr() (uintptr, error) {
	hostStubOnce.Do(func() {
		mem, err := mmapExec(hostCallStub)
		if err != nil {
			hostStubErr = err
			return
		}
		hostStubPtr = slicePtr(mem) // retained for the life of the process
	})
	return hostStubPtr, hostStubErr
}

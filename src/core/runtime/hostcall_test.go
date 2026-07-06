//go:build linux && amd64

package runtime

import (
	"encoding/binary"
	"testing"
)

// Test "wasm function" stubs for the synchronous host-call protocol, assembled
// from teststubs.s (`as` + objdump). Each is entered with the WasmWrapper ABI
// (RDI=serArgs, RSI=linMem, RDX=trap, RCX=results). They establish RBX=linMem
// (the invariant hostCallStub relies on), marshal one arg into the control frame,
// `call [ctrl+hcTrampoline]`, and on resume combine the host result with a stack
// sentinel — so a wrong result means the parked stack was not preserved across
// the Go round trip. Control-frame offsets are hard-coded (hcArgs=0x48,
// hcImportIdx=0x40, hcNArgs=0x44, hcTrampoline=0x38, hcResults=0xc8).

// stubHostRoundtrip: single frame. result = host(serArgs[0]) + 111 (its stack
// sentinel).
var stubHostRoundtrip = []byte{
	0x48, 0x89, 0xf3, // mov %rsi,%rbx        ; RBX = linMem
	0x49, 0x89, 0xce, // mov %rcx,%r14        ; stash results ptr
	0x48, 0x83, 0xec, 0x10, // sub $0x10,%rsp
	0xc7, 0x04, 0x24, 0x6f, 0x00, 0x00, 0x00, // movl $0x6f,(%rsp)  ; sentinel 111
	0x4c, 0x8b, 0x4b, 0xd8, // mov -0x28(%rbx),%r9   ; ctrl
	0x8b, 0x07, // mov (%rdi),%eax                    ; serArgs[0]
	0x49, 0x89, 0x41, 0x48, // mov %rax,0x48(%r9)     ; hcArgs[0]
	0x41, 0xc7, 0x41, 0x40, 0x07, 0x00, 0x00, 0x00, // movl $7,0x40(%r9)   ; hcImportIdx
	0x41, 0xc7, 0x41, 0x44, 0x01, 0x00, 0x00, 0x00, // movl $1,0x44(%r9)   ; hcNArgs
	0x41, 0xff, 0x51, 0x38, // call *0x38(%r9)        ; hcTrampoline
	0x4c, 0x8b, 0x4b, 0xd8, // mov -0x28(%rbx),%r9    ; reload ctrl
	0x41, 0x8b, 0x81, 0xc8, 0x00, 0x00, 0x00, // mov 0xc8(%r9),%eax  ; hcResults[0]
	0x03, 0x04, 0x24, // add (%rsp),%eax              ; + sentinel
	0x48, 0x83, 0xc4, 0x10, // add $0x10,%rsp
	0x41, 0x89, 0x06, // mov %eax,(%r14)              ; results[0]
	0xc3, // ret
}

// stubHostNested: outer frame calls inner; inner performs the host call. Proves
// both frames survive the round trip. result = host(serArgs[0]) + 100 (inner) +
// 222 (outer). The entry is stubHostNested; inner follows contiguously (the
// relative `call inner` depends on that layout).
var stubHostNested = []byte{
	// stubHostNested:
	0x48, 0x89, 0xf3, // mov %rsi,%rbx
	0x49, 0x89, 0xce, // mov %rcx,%r14
	0x4c, 0x8b, 0x2f, // mov (%rdi),%r13      ; serArgs[0] (must survive)
	0x48, 0x83, 0xec, 0x10, // sub $0x10,%rsp
	0xc7, 0x04, 0x24, 0xde, 0x00, 0x00, 0x00, // movl $0xde,(%rsp)  ; outer sentinel 222
	0xe8, 0x0b, 0x00, 0x00, 0x00, // call inner (+0x0b)
	0x03, 0x04, 0x24, // add (%rsp),%eax      ; + outer sentinel
	0x48, 0x83, 0xc4, 0x10, // add $0x10,%rsp
	0x41, 0x89, 0x06, // mov %eax,(%r14)
	0xc3, // ret
	// inner:
	0x48, 0x83, 0xec, 0x10, // sub $0x10,%rsp
	0xc7, 0x04, 0x24, 0x64, 0x00, 0x00, 0x00, // movl $0x64,(%rsp)  ; inner sentinel 100
	0x4c, 0x8b, 0x4b, 0xd8, // mov -0x28(%rbx),%r9
	0x4d, 0x89, 0x69, 0x48, // mov %r13,0x48(%r9)    ; hcArgs[0]
	0x41, 0xc7, 0x41, 0x40, 0x07, 0x00, 0x00, 0x00, // movl $7,0x40(%r9)
	0x41, 0xc7, 0x41, 0x44, 0x01, 0x00, 0x00, 0x00, // movl $1,0x44(%r9)
	0x41, 0xff, 0x51, 0x38, // call *0x38(%r9)
	0x4c, 0x8b, 0x4b, 0xd8, // mov -0x28(%rbx),%r9   ; reload ctrl
	0x41, 0x8b, 0x81, 0xc8, 0x00, 0x00, 0x00, // mov 0xc8(%r9),%eax
	0x03, 0x04, 0x24, // add (%rsp),%eax
	0x48, 0x83, 0xc4, 0x10, // add $0x10,%rsp
	0xc3, // ret
}

// hostCallFixture allocates the buffers a CallWithHost run needs and installs the
// control frame as the import ctx.
func hostCallFixture(t *testing.T, jm *JobMemory, ar *Arena) (serArgs, results, trap, ctrl []byte) {
	t.Helper()
	serArgs = ar.Alloc(16)
	results = ar.Alloc(16)
	trap = ar.Alloc(8)
	ctrl = ar.Alloc(ctrlFrameSize)
	jm.SetCustomCtx(slicePtr(ctrl))
	return
}

// TestHostCallRoundtrip: native marshals one arg, the Go host doubles it, native
// resumes and adds its stack sentinel. double(20)+111 == 151, host called once
// with importIdx 7 and arg 20.
func TestHostCallRoundtrip(t *testing.T) {
	eng, jm, ar := fixture(t)
	code, err := mmapExec(stubHostRoundtrip)
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)

	serArgs, results, trap, ctrl := hostCallFixture(t, jm, ar)
	binary.LittleEndian.PutUint32(serArgs, 20)

	calls := 0
	var sawImport uint32
	var sawArg uint64
	host := func(imp uint32, args, res []uint64) {
		calls++
		sawImport = imp
		sawArg = args[0]
		res[0] = args[0] * 2
	}

	if err := eng.CallWithHost(slicePtr(code), serArgs, jm.LinearMemory(), trap, results, ctrl, host); err != nil {
		t.Fatalf("CallWithHost: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host fn invoked %d times, want 1", calls)
	}
	if sawImport != 7 || sawArg != 20 {
		t.Fatalf("host saw importIdx=%d arg=%d, want 7 and 20", sawImport, sawArg)
	}
	if got := binary.LittleEndian.Uint32(results); got != 151 {
		t.Fatalf("round-trip result = %d, want 151 (double(20)+111 sentinel)", got)
	}
}

// TestHostCallDeepStack: the host call happens inside a nested call, so the
// parked foreign stack holds two frames (both sentinels + the outer return
// address). If resume clobbered or mis-restored the stack, the sentinels would be
// wrong. double(20)+100+222 == 362.
func TestHostCallDeepStack(t *testing.T) {
	eng, jm, ar := fixture(t)
	code, err := mmapExec(stubHostNested)
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)

	serArgs, results, trap, ctrl := hostCallFixture(t, jm, ar)
	binary.LittleEndian.PutUint32(serArgs, 20)

	calls := 0
	host := func(imp uint32, args, res []uint64) {
		calls++
		res[0] = args[0] * 2
	}

	if err := eng.CallWithHost(slicePtr(code), serArgs, jm.LinearMemory(), trap, results, ctrl, host); err != nil {
		t.Fatalf("CallWithHost: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host fn invoked %d times, want 1", calls)
	}
	if got := binary.LittleEndian.Uint32(results); got != 362 {
		t.Fatalf("deep-stack result = %d, want 362 (double(20)+100+222 sentinels)", got)
	}
}

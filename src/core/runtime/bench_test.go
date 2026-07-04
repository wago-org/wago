//go:build linux && amd64

package runtime

import (
	"encoding/binary"
	"testing"
)

// BenchmarkCrossBoundaryCall measures the full host->wasm->host round-trip cost
// of Engine.Call: marshal a pointer arg, switch to the foreign stack via the asm
// trampoline, run native code (add1), switch back, and read the result + trap.
// This is the cross-boundary latency that should rival wazero.
func BenchmarkCrossBoundaryCall(b *testing.B) {
	eng, err := NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	jm, err := NewJobMemory(linMemBytes)
	if err != nil {
		b.Fatal(err)
	}
	defer jm.Close()
	ar, err := NewArena(4096)
	if err != nil {
		b.Fatal(err)
	}
	defer ar.Close()
	code, err := mmapExec(stubAdd1)
	if err != nil {
		b.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)

	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	lin := jm.LinearMemory()
	binary.LittleEndian.PutUint32(serArgs, 41)
	codePtr := slicePtr(code)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := eng.Call(codePtr, serArgs, lin, trap, results); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if got := binary.LittleEndian.Uint32(results); got != 42 {
		b.Fatalf("result = %d, want 42", got)
	}
}

// BenchmarkHostCall measures a wasm->host->wasm round-trip via the re-entry
// protocol: native signals a pending host call (first foreign-stack crossing),
// Go runs the host function on the goroutine stack, then native is re-entered to
// resume (second crossing). So this is ~2x BenchmarkCrossBoundaryCall plus the
// Go closure invocation.
func BenchmarkHostCall(b *testing.B) {
	eng, err := NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	jm, err := NewJobMemory(linMemBytes)
	if err != nil {
		b.Fatal(err)
	}
	defer jm.Close()
	ar, err := NewArena(4096)
	if err != nil {
		b.Fatal(err)
	}
	defer ar.Close()
	code, err := mmapExec(stubHostRoundtrip)
	if err != nil {
		b.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)

	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	ctrl := ar.Alloc(ctrlFrameSize)
	jm.SetCustomCtx(slicePtr(ctrl))
	lin := jm.LinearMemory()
	binary.LittleEndian.PutUint32(serArgs, 20)
	codePtr := slicePtr(code)
	host := func(imp uint32, args, res []uint64) { res[0] = args[0] * 2 }

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := eng.CallWithHost(codePtr, serArgs, lin, trap, results, ctrl, host); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if got := binary.LittleEndian.Uint32(results); got != 151 {
		b.Fatalf("result = %d, want 151 (double(20)+111 sentinel)", got)
	}
}

// BenchmarkLinearMemoryAccess confirms host-side linear-memory access is just a
// slice index over the shared mmap (zero-copy, no syscall, no marshalling).
func BenchmarkLinearMemoryAccess(b *testing.B) {
	jm, err := NewJobMemory(linMemBytes)
	if err != nil {
		b.Fatal(err)
	}
	defer jm.Close()
	lin := jm.LinearMemory()
	b.ReportAllocs()
	b.ResetTimer()
	var sum uint32
	for i := 0; i < b.N; i++ {
		off := (i * 4) & (linMemBytes - 4)
		binary.LittleEndian.PutUint32(lin[off:], uint32(i))
		sum += binary.LittleEndian.Uint32(lin[off:])
	}
	_ = sum
}

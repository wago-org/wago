//go:build linux && (amd64 || arm64)

package runtime

import (
	"syscall"
	"testing"
	"unsafe"
)

func TestFilePageSnapshotDiscardRestoresPrivateMapping(t *testing.T) {
	want := make([]byte, pageSize*2)
	for i := range want {
		want[i] = byte(i*31 + 7)
	}
	backing, err := newPageSnapshotBacking(want)
	if err != nil {
		t.Fatalf("newPageSnapshotBacking: %v", err)
	}
	defer backing.close()

	memory, err := mmapRW(len(want))
	if err != nil {
		t.Fatalf("mmapRW: %v", err)
	}
	defer syscall.Munmap(memory)
	addr := uintptr(unsafe.Pointer(&memory[0]))
	if err = backing.reset(addr, len(memory)); err != nil {
		t.Fatalf("reset: %v", err)
	}

	for cycle := 0; cycle < 3; cycle++ {
		for i := range memory {
			memory[i] ^= byte(cycle + 1)
		}
		if err = backing.discard(addr, len(memory)); err != nil {
			t.Fatalf("discard cycle %d: %v", cycle, err)
		}
		for i := range memory {
			if memory[i] != want[i] {
				t.Fatalf("discard cycle %d byte %d: got %#x, want %#x", cycle, i, memory[i], want[i])
			}
		}
	}
}

func BenchmarkFilePageSnapshotReset20MiB(b *testing.B) {
	benchmarkFilePageSnapshot20MiB(b, false)
}

func BenchmarkFilePageSnapshotDiscard20MiB(b *testing.B) {
	benchmarkFilePageSnapshot20MiB(b, true)
}

func benchmarkFilePageSnapshot20MiB(b *testing.B, discard bool) {
	const size = 20 << 20
	image := make([]byte, size)
	backing, err := newPageSnapshotBacking(image)
	if err != nil {
		b.Fatalf("newPageSnapshotBacking: %v", err)
	}
	b.Cleanup(func() { _ = backing.close() })
	memory, err := mmapRW(size)
	if err != nil {
		b.Fatalf("mmapRW: %v", err)
	}
	b.Cleanup(func() { _ = syscall.Munmap(memory) })
	addr := uintptr(unsafe.Pointer(&memory[0]))
	if err = backing.reset(addr, len(memory)); err != nil {
		b.Fatalf("initial reset: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		memory[0] = byte(i)
		if discard {
			err = backing.discard(addr, len(memory))
		} else {
			err = backing.reset(addr, len(memory))
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

//go:build linux && riscv64

package riscv64spike

import (
	"fmt"
	goruntime "runtime"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	rv "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// TestCodePublicationAcrossOSThreads publishes fresh code repeatedly, then asks
// a fixed set of locked OS threads to execute every mapping. On native hardware
// this exercises Linux's process-wide riscv_flush_icache path across harts; under
// QEMU it remains a deterministic publication/lifetime correctness test.
func TestCodePublicationAcrossOSThreads(t *testing.T) {
	workers := goruntime.GOMAXPROCS(0)
	if workers > 8 {
		workers = 8
	}
	if workers < 2 {
		workers = 2
	}
	type task struct {
		entry uintptr
		want  uintptr
		done  chan<- error
	}
	work := make(chan task)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			goruntime.LockOSThread()
			defer goruntime.UnlockOSThread()
			for job := range work {
				got := Call2(job.entry, 0, 0)
				if got != job.want {
					job.done <- &publicationMismatch{got: got, want: job.want}
				} else {
					job.done <- nil
				}
			}
		}()
	}
	defer func() {
		close(work)
		wg.Wait()
	}()

	const iterations = 64
	for i := uintptr(1); i <= iterations; i++ {
		want := uintptr(0x5a000000) | i
		var a rv.Asm
		a.MovImm64(rv.A0, uint64(want))
		a.Ret()
		mem, err := MapExec(a.B)
		if err != nil {
			t.Fatal(err)
		}
		entry := uintptr(unsafe.Pointer(&mem[0]))
		done := make(chan error, workers)
		for range workers {
			work <- task{entry: entry, want: want, done: done}
		}
		for range workers {
			if err := <-done; err != nil {
				_ = syscall.Munmap(mem)
				t.Fatal(err)
			}
		}
		if err := syscall.Munmap(mem); err != nil {
			t.Fatal(err)
		}
	}
}

type publicationMismatch struct{ got, want uintptr }

func (e *publicationMismatch) Error() string {
	return fmt.Sprintf("published code returned %#x, want %#x", e.got, e.want)
}

//go:build wago_inline_host_experiment && (linux || darwin) && arm64 && !tinygo

package runtime

import (
	"encoding/binary"
	"os"
	goruntime "runtime"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func inlineProbeFixture(t testing.TB, looping bool) func(func(uint64) uint64, uint64) uint64 {
	t.Helper()
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	memory, err := mmapRW(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { munmap(memory) })
	var a a64.Asm
	a.StpPre(a64.X19, a64.LR, a64.SP, -16)
	if looping {
		a.StpPre(a64.X20, a64.X21, a64.SP, -16)
	}
	a.MovReg64(a64.X19, a64.X3)
	a.MovReg64(a64.X26, a64.X1)
	if looping {
		mustEncode(a.Load64(a64.X20, a64.X0, 0))
		a.MovImm64(a64.X21, 0)
	} else {
		mustEncode(a.Load64(a64.X0, a64.X0, 0))
	}
	loopStart := a.Len()
	if looping {
		a.MovReg64(a64.X0, a64.X21)
	}
	a.MovImm64(a64.X16, uint64(inlineHostProbeBridgeAddr()))
	a.Blr(a64.X16)
	if looping {
		a.MovReg64(a64.X21, a64.X0)
		a.SubImm64(a64.X20, a64.X20, 1)
		again := a.Cbnz64(a64.X20)
		mustEncode(a.PatchBranch19(again, loopStart))
	}
	mustEncode(a.Store64(a64.X0, a64.X19, 0))
	if looping {
		a.LdpPost(a64.X20, a64.X21, a64.SP, 16)
	}
	a.LdpPost(a64.X19, a64.LR, a64.SP, 16)
	a.Ret()
	code, err := mmapExec(a.B)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { munmap(code) })
	args, result := make([]byte, 8), make([]byte, 8)
	return func(fn func(uint64) uint64, value uint64) uint64 {
		binary.LittleEndian.PutUint64(args, value)
		inlineHostProbe(fn, slicePtr(code), slicePtr(args), slicePtr(memory)+512, slicePtr(result), e.StackTop())
		goruntime.KeepAlive(fn)
		goruntime.KeepAlive(e)
		goruntime.KeepAlive(memory)
		goruntime.KeepAlive(code)
		goruntime.KeepAlive(args)
		return binary.LittleEndian.Uint64(result)
	}
}

//go:noinline
func inlineProbeGrow(depth int) uint64 {
	var buffer [2048]byte
	buffer[0] = byte(depth)
	if depth == 0 {
		goruntime.GC()
		return 0
	}
	value := inlineProbeGrow(depth-1) + uint64(buffer[0])
	goruntime.KeepAlive(&buffer)
	return value
}

func TestInlineHostProbeRealGoStack(t *testing.T) {
	call := inlineProbeFixture(t, false)
	var counter atomic.Uint64
	fn := func(v uint64) uint64 { counter.Add(1); return v + 1 }
	if got := call(fn, 41); got != 42 || counter.Load() != 1 {
		t.Fatalf("result %d, counter %d", got, counter.Load())
	}
	grew := false
	got := call(func(v uint64) uint64 {
		var pcs [64]uintptr
		frames := goruntime.CallersFrames(pcs[:goruntime.Callers(0, pcs[:])])
		found := false
		for {
			f, more := frames.Next()
			if strings.Contains(f.Function, "inlineHostProbe") {
				found = true
			}
			if !more {
				break
			}
		}
		if !found {
			t.Fatal("live Go bridge frame missing from stack trace")
		}
		if inlineProbeGrow(32) != 528 {
			t.Fatal("growth result")
		}
		grew = true
		return v + 1
	}, 91)
	if got != 92 || !grew {
		t.Fatalf("growing callback %d", got)
	}
	for i := uint64(0); i < 100; i++ {
		if got := call(fn, i); got != i+1 {
			t.Fatalf("post-growth %d", got)
		}
	}
}

func BenchmarkInlineHostProbeAtomic(b *testing.B) {
	call := inlineProbeFixture(b, false)
	var counter atomic.Uint64
	fn := func(v uint64) uint64 { counter.Add(1); return v + 1 }
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := call(fn, uint64(i)); got != uint64(i)+1 {
			b.Fatal(got)
		}
	}
	b.StopTimer()
	if counter.Load() != uint64(b.N) {
		b.Fatal("wrong callback count")
	}
}

func TestInlineHostProbeBlockingAndPanic(t *testing.T) {
	call := inlineProbeFixture(t, false)
	old := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(old)
	waiting, release, done := make(chan struct{}), make(chan struct{}), make(chan uint64, 1)
	go func() {
		done <- call(func(v uint64) uint64 { close(waiting); <-release; goruntime.GC(); return v + 1 }, 12)
	}()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("callback did not block")
	}
	goruntime.GC()
	close(release)
	select {
	case v := <-done:
		if v != 13 {
			t.Fatal(v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("callback did not resume")
	}
	recovered := false
	func() {
		defer func() { recovered = recover() == "probe panic" }()
		call(func(uint64) uint64 { panic("probe panic") }, 0)
	}()
	if !recovered {
		t.Fatal("panic did not unwind through live Go frame")
	}
	if got := call(func(v uint64) uint64 { return v + 1 }, 20); got != 21 {
		t.Fatal(got)
	}
}

// Independent engines have independent foreign stacks and landing records.
// Same-engine reentry still needs the production stack-checkpoint protocol.
func TestInlineHostProbeNestedIndependentStacks(t *testing.T) {
	outer := inlineProbeFixture(t, false)
	inner := inlineProbeFixture(t, false)
	type payload struct {
		value   uint64
		padding [4096]byte
	}
	root := &payload{value: 17}
	var count atomic.Uint64
	callback := func(v uint64) uint64 {
		return inner(func(w uint64) uint64 {
			if inlineProbeGrow(32) != 528 {
				t.Fatal("nested stack growth")
			}
			count.Add(1)
			return w + root.value
		}, v) + 1
	}
	for i := uint64(0); i < 10; i++ {
		if got := outer(callback, i); got != i+18 {
			t.Fatalf("nested result %d", got)
		}
	}
	if count.Load() != 10 {
		t.Fatal("nested callback count")
	}
	goruntime.KeepAlive(root)
}

func TestInlineHostProbeCPUProfile(t *testing.T) {
	call := inlineProbeFixture(t, true)
	file, err := os.CreateTemp(t.TempDir(), "cpu-profile")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := pprof.StartCPUProfile(file); err != nil {
		t.Fatal(err)
	}
	defer pprof.StopCPUProfile()
	var counter atomic.Uint64
	fn := func(v uint64) uint64 { counter.Add(1); return v + 1 }
	const count = 65536
	for i := 0; i < 1024; i++ {
		if got := call(fn, count); got != count {
			t.Fatal(got)
		}
	}
	if counter.Load() != 1024*count {
		t.Fatal("profiled callback count")
	}
}

func BenchmarkInlineHostProbeAtomicBatch(b *testing.B) {
	call := inlineProbeFixture(b, true)
	var counter atomic.Uint64
	fn := func(v uint64) uint64 { counter.Add(1); return v + 1 }
	const count = 65536
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := call(fn, count); got != count {
			b.Fatal(got)
		}
	}
	b.StopTimer()
	total := uint64(b.N) * count
	if counter.Load() != total {
		b.Fatal("wrong callback count")
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(total), "ns/call")
}

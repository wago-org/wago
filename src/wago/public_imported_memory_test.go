//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Both provider and consumer contain real scalar functions. Sharing a Memory
// object does not request the Wasm threads/shared-memory feature.
func publicImportedMemoryModule(imported bool, initial byte, passive bool, badActive bool) []byte {
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, nil),
	))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		entry = append(entry, 2, 1, 1, 4)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{0}, []byte{2}, []byte{1}, []byte{2})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 4}))
	}
	if initial != 0 {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, initial, 0x0b}))))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(
		wasmtest.ExportEntry("bump", 0, 0), wasmtest.ExportEntry("grow", 0, 1),
		wasmtest.ExportEntry("size", 0, 2), wasmtest.ExportEntry("drop", 0, 3),
		wasmtest.ExportEntry("init", 0, 4), wasmtest.ExportEntry("write", 0, 5),
		wasmtest.ExportEntry("memory", 2, 0),
	)))
	if passive {
		sections = append(sections, wasmtest.Section(12, []byte{1}))
	}
	bump := []byte{0x41, 7, 0x0b}
	if initial != 0 {
		bump = []byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x23, 0, 0x0b}
	}
	drop, init := []byte{0x0b}, []byte{0x20, 0, 0x2d, 0, 0, 0x0b}
	if passive {
		drop = []byte{0xfc, 9, 0, 0x0b}
		init = []byte{0x20, 0, 0x41, 0, 0x41, 1, 0xfc, 8, 0, 0, 0x20, 0, 0x2d, 0, 0, 0x0b}
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code(bump), wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}),
		wasmtest.Code([]byte{0x3f, 0, 0x0b}), wasmtest.Code(drop), wasmtest.Code(init),
		wasmtest.Code([]byte{0x41, 0, 0x41, 0x23, 0x3a, 0, 0, 0x0b}),
	)))
	if passive {
		sections = append(sections, wasmtest.Section(11, []byte{1, 1, 1, 0x5a}))
	}
	if badActive {
		// Active byte at 65536, just past the initial page.
		sections = append(sections, wasmtest.Section(11, []byte{1, 0, 0x41, 0x80, 0x80, 4, 0x0b, 1, 0x5a}))
	}
	return wasmtest.Module(sections...)
}

func importedMemoryCompile(t *testing.T, imported bool, initial byte, passive, bad bool, quota uint32) *Compiled {
	t.Helper()
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(quota), publicImportedMemoryModule(imported, initial, passive, bad))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func importedMemoryInvoke(t *testing.T, in *Instance, name string, want uint64, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(name, args...)
	if err != nil || !slices.Equal(got, []uint64{want}) {
		t.Fatalf("%s(%v) = %v, %v; want %#x", name, args, got, err, want)
	}
}

func importedMemoryProvider(t *testing.T) (*Instance, *Memory) {
	t.Helper()
	p, err := Instantiate(importedMemoryCompile(t, false, 10, true, false, 0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	m, err := p.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	return p, m
}

func importedMemoryWaitAttachments(t *testing.T, m *Memory, count uint32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := m.state.Load()
		s.mu.Lock()
		n := s.importerCount()
		s.mu.Unlock()
		if n == count {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("constructor did not reach the memory attachment boundary")
}

func TestPublicImportedMemoryConstructionLeaseAndRollback(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed-active-initializer"}[bad], func(t *testing.T) {
			p, memory := importedMemoryProvider(t)
			c := importedMemoryCompile(t, true, 20, false, bad, 0)
			importedMemoryInvoke(t, p, "bump", 11)
			nativeExecutionMu.Lock()
			locked := true
			defer func() {
				if locked {
					nativeExecutionMu.Unlock()
				}
			}()
			before := p.jm.CaptureInstanceContext()
			type outcome struct {
				in  *Instance
				err error
			}
			done := make(chan outcome, 1)
			go func() {
				in, err := Instantiate(c, NewImports().Memory("env", "memory", memory))
				done <- outcome{in, err}
			}()
			importedMemoryWaitAttachments(t, memory, 1)
			if got := p.jm.CaptureInstanceContext(); got != before {
				t.Fatal("construction changed a tenant's context while execution lease was held")
			}
			select {
			case <-done:
				t.Fatal("construction escaped the execution lease")
			default:
			}
			nativeExecutionMu.Unlock()
			locked = false
			select {
			case got := <-done:
				if bad {
					if got.in != nil || got.err == nil {
						t.Fatal("bad initializer succeeded", got.err)
					}
					if p.jm.CaptureInstanceContext() != before {
						t.Fatal("failed construction retained new context pointers")
					}
					importedMemoryWaitAttachments(t, memory, 0)
				} else {
					if got.err != nil {
						t.Fatal(got.err)
					}
					defer got.in.Close()
					importedMemoryInvoke(t, got.in, "bump", 21)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("construction did not release its lease")
			}
			importedMemoryInvoke(t, p, "bump", 12)
		})
	}
}

func TestPublicImportedMemoryAbsentContextAndPrivateData(t *testing.T) {
	p, memory := importedMemoryProvider(t)
	low, err := Instantiate(importedMemoryCompile(t, true, 20, true, false, 2), NewImports().Memory("env", "memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer low.Close()
	importedMemoryInvoke(t, low, "grow", 1, 1)
	plain, err := Instantiate(importedMemoryCompile(t, true, 0, false, false, 0), NewImports().Memory("env", "memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	ctx := unsafe.Slice((*byte)(offHeapPtr(plain.nativeContext)), coreruntime.InstanceContextBytes)
	for _, offset := range []int{32, 40, 56} {
		if binary.LittleEndian.Uint64(ctx[offset:]) != 0 {
			t.Fatalf("absent context pointer at %d inherited another tenant", offset)
		}
	}
	importedMemoryInvoke(t, p, "init", 0x5a, 1)
	if _, err := low.Invoke("drop"); err != nil {
		t.Fatal(err)
	}
	if _, err := low.Invoke("init", 1); err == nil {
		t.Fatal("dropped consumer data remained available")
	}
	importedMemoryInvoke(t, p, "init", 0x5a, 2)
	if err := low.Close(); err != nil {
		t.Fatal(err)
	}
	// The quota tenant's arena can now be recycled; plain must not borrow it.
	importedMemoryInvoke(t, plain, "grow", 2, 1)
	importedMemoryInvoke(t, p, "size", 3)
	importedMemoryInvoke(t, plain, "grow", 3, 0)
	importedMemoryInvoke(t, plain, "grow", 0xffffffff, 0xffffffff00000002)
	importedMemoryInvoke(t, plain, "size", 3)
}

func TestPublicImportedMemoryQueuedCancellationSurvivesForeignTrap(t *testing.T) {
	p, memory := importedMemoryProvider(t)
	in, err := Instantiate(importedMemoryCompile(t, true, 20, false, false, 0), NewImports().Memory("env", "memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	importedMemoryInvoke(t, p, "bump", 11) // installs the provider's trap pointer
	nativeExecutionMu.Lock()
	locked := true
	defer func() {
		if locked {
			nativeExecutionMu.Unlock()
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "write"); done <- err }()
	// Publish an interruption only after invocation admission, while it is
	// queued on the foreign activation's execution lease. This deterministic
	// sentinel models a watcher that has already exhausted its bounded retries.
	deadline := time.Now().Add(3 * time.Second)
	for in.invocationState.Load()&instanceInvocationCount == 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if in.invocationState.Load()&instanceInvocationCount == 0 {
		t.Fatal("queued invocation never entered admission")
	}
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&in.trap[0])), uint32(coreruntime.TrapInterrupted))
	nativeExecutionMu.Unlock()
	locked = false
	select {
	case err := <-done:
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapInterrupted {
			t.Fatal("queued interruption was erased", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued interrupted entry did not unwind")
	}
	if runtime.GOARCH == "riscv64" && memory.UnsafeBytes()[0] != 0 {
		t.Fatal("interrupted guest executed its first write")
	}
	importedMemoryInvoke(t, in, "bump", 21)
}

func TestPublicImportedMemoryQuotaRecheckedAfterLeaseWait(t *testing.T) {
	p, memory := importedMemoryProvider(t)
	c := importedMemoryCompile(t, true, 20, false, false, 2)
	nativeExecutionMu.Lock()
	locked := true
	defer func() {
		if locked {
			nativeExecutionMu.Unlock()
		}
	}()
	done := make(chan error, 1)
	go func() {
		in, err := Instantiate(c, NewImports().Memory("env", "memory", memory))
		if in != nil {
			_ = in.Close()
		}
		done <- err
	}()
	importedMemoryWaitAttachments(t, memory, 1)
	// Execute the provider's real growth entry using the execution lease already
	// owned by this test. The constructor has passed preflight and is waiting.
	if err := p.bindNativeContext(); err != nil {
		t.Fatal(err)
	}
	railshotBindNativeInvocation(p)
	binary.LittleEndian.PutUint64(p.serArgs, 2)
	if err := callNative(p.c, p.eng, p.jm, true, p.base+uintptr(p.c.Entry[1]), p.serArgs, p.trap, p.results); err != nil {
		t.Fatal(err)
	}
	if p.jm.CurrentPages() != 3 {
		t.Fatal("provider did not grow while constructor waited")
	}
	nativeExecutionMu.Unlock()
	locked = false
	select {
	case err := <-done:
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatal("stale quota preflight admitted grown backing", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("quota rejection retained execution lease")
	}
	importedMemoryWaitAttachments(t, memory, 0)
	importedMemoryInvoke(t, p, "bump", 11)
}

func TestPublicImportedMemoryArtifactAndRetainedOwner(t *testing.T) {
	p, memory := importedMemoryProvider(t)
	c := importedMemoryCompile(t, true, 20, true, false, 0)
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	a, err := Instantiate(loaded, NewImports().Memory("env", "memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(c, NewImports().Memory("env", "memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := loaded.Close(); err != nil {
		t.Fatal(err)
	}
	reexport, err := a.ExportedMemory("memory")
	if err != nil || reexport != memory {
		t.Fatal("re-export changed memory identity", err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if memory.jobMemory() == nil {
		t.Fatal("producer close unmapped retained imported memory")
	}
	if late, err := Instantiate(c, NewImports().Memory("env", "memory", reexport)); late != nil || err == nil {
		t.Fatal("new consumer attached a closed producer")
	}
	importedMemoryInvoke(t, a, "bump", 21)
	importedMemoryInvoke(t, b, "bump", 21)
	importedMemoryInvoke(t, a, "bump", 22)
	importedMemoryInvoke(t, b, "bump", 22)
	importedMemoryInvoke(t, a, "grow", 1, 1)
	importedMemoryInvoke(t, b, "size", 2)
	fn, err := b.WasmFunc("bump")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fn.Invoke(); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() {
		if _, err := fn.Invoke(); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatalf("warm imported memory call allocations = %g", n)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	importedMemoryInvoke(t, b, "size", 2)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if memory.jobMemory() != nil {
		t.Fatal("last borrower did not release producer mapping")
	}
}

func TestPublicImportedMemoryHostOwnershipAndLimits(t *testing.T) {
	c := importedMemoryCompile(t, true, 20, false, false, 0)
	if in, err := Instantiate(c); in != nil || err == nil {
		t.Fatal("missing memory binding accepted")
	}
	for _, max := range []uint32{4, 5} {
		m, err := NewMemory(1, max)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, NewImports().Memory("env", "memory", m))
		if max == 5 {
			if in != nil || err == nil {
				t.Fatal("incompatible declared maximum accepted")
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Close(); err == nil {
				t.Fatal("host closed live imported backing")
			}
			if second, err := Instantiate(c, NewImports().Memory("env", "memory", m)); second != nil || err == nil {
				t.Fatal("exclusive host memory attached twice")
			}
			importedMemoryInvoke(t, in, "grow", 1, 0)
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.Close(); err != nil {
			t.Fatal("failed construction leaked memory attachment", err)
		}
	}
	shared, err := NewSharedMemory(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	// Host sharing is broader than the Wasm threads type. Establish an exact
	// shared Wasm export before testing the incompatible import declaration.
	if err := shared.share(nil, memoryDef{Min: 1, Max: 4, HasMax: true, Shared: true}); err != nil {
		t.Fatal(err)
	}
	if in, err := Instantiate(c, NewImports().Memory("env", "memory", shared)); in != nil || err == nil {
		t.Fatal("Wasm shared memory admitted for nonshared import")
	}
}

// Hiding the stdlib cancelCtx lookup routes context.AfterFunc through this
// registration acknowledgment. It witnesses the actual invocation watcher,
// after context-sensitive admission, before canceling a queued entry.
type importedMemoryCancellationContext struct {
	context.Context
	armed chan struct{}
}

func (c importedMemoryCancellationContext) Value(any) any { return nil }
func (c importedMemoryCancellationContext) AfterFunc(f func()) func() bool {
	stop := context.AfterFunc(c.Context, f)
	close(c.armed)
	return stop
}

func TestPublicImportedMemoryQueuedContextCancellationAndClose(t *testing.T) {
	for _, action := range []string{"context", "close"} {
		t.Run(action, func(t *testing.T) {
			p, memory := importedMemoryProvider(t)
			in, err := Instantiate(importedMemoryCompile(t, true, 20, false, false, 0), NewImports().Memory("env", "memory", memory))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			importedMemoryInvoke(t, p, "bump", 11)
			nativeExecutionMu.Lock()
			locked := true
			defer func() {
				if locked {
					nativeExecutionMu.Unlock()
				}
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			watched := importedMemoryCancellationContext{Context: ctx, armed: make(chan struct{})}
			done := make(chan error, 1)
			go func() { _, err := in.InvokeContext(watched, "write"); done <- err }()
			select {
			case <-watched.armed:
			case <-time.After(3 * time.Second):
				t.Fatal("queued invocation watcher was not registered")
			}
			deadline := time.Now().Add(3 * time.Second)
			for in.invocationState.Load()&instanceInvocationCount == 0 && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			if in.invocationState.Load()&instanceInvocationCount == 0 {
				t.Fatal("queued entry was not admitted")
			}
			var closed chan error
			if action == "context" {
				cancel()
			} else {
				closed = make(chan error, 1)
				go func() { closed <- in.Close() }()
			}
			for atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			if atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
				t.Fatal("queued cancellation did not publish interruption")
			}
			nativeExecutionMu.Unlock()
			locked = false
			select {
			case err := <-done:
				var trap *TrapError
				if action == "context" {
					if !errors.Is(err, context.Canceled) {
						t.Fatal("queued context cancellation lost", err)
					}
				} else if !errors.As(err, &trap) || trap.Code != TrapInterrupted {
					t.Fatal("queued close interruption lost", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued canceled borrower did not unwind")
			}
			if runtime.GOARCH == "riscv64" && memory.UnsafeBytes()[0] != 0 {
				t.Fatal("canceled borrower executed its first write")
			}
			if closed != nil {
				select {
				case err := <-closed:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("borrower Close did not wait for foreign completion")
				}
			} else {
				importedMemoryInvoke(t, in, "bump", 21)
			}
			importedMemoryInvoke(t, p, "bump", 12)
		})
	}
}

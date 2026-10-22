//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func permissions(t *testing.T, base uintptr) string {
	t.Helper()
	data, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		parts := strings.Split(fields[0], "-")
		if len(parts) != 2 {
			continue
		}
		lo, e1 := strconv.ParseUint(parts[0], 16, 64)
		hi, e2 := strconv.ParseUint(parts[1], 16, 64)
		if e1 == nil && e2 == nil && uint64(base) >= lo && uint64(base) < hi {
			return fields[1]
		}
	}
	return ""
}

func testMemory() *memoryOps {
	ops := linuxMemory
	ops.flush = func([]byte, int) error { return nil } // no generated entry in these portable tests
	return &ops
}

func snippet(add int32) []byte {
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -16)
	a.Sd(enc.RA, enc.SP, 0)
	a.Add(enc.X10, enc.X10, enc.X11)
	a.Addi(enc.X10, enc.X10, add)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 16)
	a.Ret()
	if a.Err() != nil {
		panic(a.Err())
	}
	return a.B
}

func TestPublicationOrderAndWX(t *testing.T) {
	ops := testMemory()
	var events []string
	protect := ops.protectRX
	ops.protectRX = func(mem []byte) error {
		if got := permissions(t, pointer(mem)); !strings.HasPrefix(got, "rw-") {
			t.Fatalf("staging permissions %q", got)
		}
		events = append(events, "RX")
		return protect(mem)
	}
	ops.flush = func(mem []byte, used int) error {
		if got := permissions(t, pointer(mem)); !strings.HasPrefix(got, "r-x") {
			t.Fatalf("flush permissions %q", got)
		}
		if used != len(snippet(1)) {
			t.Fatalf("flush logical size %d", used)
		}
		events = append(events, "flush")
		return nil
	}
	b, err := newCodeBuffer(1, 8192, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = b.Append(snippet(1)); err != nil {
		t.Fatal(err)
	}
	p, err := publishImage(b, len(b.Bytes()), ops)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if strings.Join(events, ",") != "RX,flush" {
		t.Fatal(events)
	}
	if _, _, err = b.Take(); !errors.Is(err, ErrClosed) {
		t.Fatal("producer did not transfer once", err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	if permissions(t, pointer(p.mem)) == "" {
		t.Fatal("producer close retired transferred image")
	}
	base := pointer(p.mem)
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
	if permissions(t, base) != "" {
		t.Fatal("closed image remains mapped")
	}
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGrowAliasedSourceAndTransfer(t *testing.T) {
	b, err := newCodeBuffer(1, 32768, testMemory())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	page := len(b.mem)
	payload := bytes.Repeat([]byte{0x5a}, page-16)
	if err = b.Append(payload); err != nil {
		t.Fatal(err)
	}
	// Include a writable tail beyond logical size, as current CodeBuffer.Append
	// supports. The source must be copied before old mapping retirement.
	for i := page - 16; i < page; i++ {
		b.mem[i] = 0xa5
	}
	oldBase := pointer(b.mem)
	source := b.mem[16:]
	want := append(append([]byte(nil), payload...), source...)
	if err = b.Append(source); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Bytes(), want) || pointer(b.mem) == oldBase {
		t.Fatal("aliased growth lost source bytes")
	}
	if permissions(t, oldBase) != "" {
		t.Fatal("old growth image remains mapped")
	}
	if err = b.Append(make([]byte, 32768)); err == nil {
		t.Fatal("logical code bound ignored")
	}
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatal("bound failure mutated bytes")
	}
	mem, base, err := b.Take()
	if err != nil || base != pointer(mem) {
		t.Fatal(err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mem[:len(want)], want) {
		t.Fatal("Take lost image")
	}
	if err = linuxMemory.unmap(mem); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationFailuresAndCleanupRetry(t *testing.T) {
	failure := errors.New("injected syscall failure")
	for _, stage := range []string{"RX", "flush", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			ops := testMemory()
			unmap := ops.unmap
			var base uintptr
			flushes := 0
			ops.flush = func([]byte, int) error {
				flushes++
				if stage != "RX" {
					return failure
				}
				return nil
			}
			if stage == "RX" {
				ops.protectRX = func([]byte) error { return failure }
			}
			ops.unmap = func(mem []byte) error {
				if stage == "cleanup" {
					return failure
				}
				return unmap(mem)
			}
			b, err := newCodeBuffer(4, 4, ops)
			if err != nil {
				t.Fatal(err)
			}
			base = pointer(b.mem)
			if err = b.Append([]byte{0x13, 0, 0, 0}); err != nil {
				t.Fatal(err)
			}
			p, err := publishImage(b, 4, ops)
			if !errors.Is(err, failure) {
				t.Fatalf("failure %v", err)
			}
			if stage == "RX" && flushes != 0 {
				t.Fatal("flush happened after failed protect")
			}
			if stage == "cleanup" {
				if p == nil || !p.closed || permissions(t, base) == "" {
					t.Fatal("failed cleanup lost retired owner")
				}
				if _, err := new(Engine).Call(p, 0, 0, 0, 16); !errors.Is(err, ErrClosed) {
					t.Fatal("failed publication callable", err)
				}
				ops.unmap = unmap
				if err = p.Close(); err != nil {
					t.Fatal(err)
				}
			} else if p != nil {
				t.Fatal("failed publication exposed an owner")
			}
			if permissions(t, base) != "" {
				t.Fatal("failed image not cleaned up")
			}
		})
	}
}

func TestGrowFailureKeepsBothCleanupOwners(t *testing.T) {
	ops := testMemory()
	failure := errors.New("injected unmap failure")
	b, err := newCodeBuffer(1, 32768, ops)
	if err != nil {
		t.Fatal(err)
	}
	page := len(b.mem)
	if err = b.Append(bytes.Repeat([]byte{1}, page)); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), b.Bytes()...)
	unmap := ops.unmap
	ops.unmap = func([]byte) error { return failure }
	if err = b.Append([]byte{2}); !errors.Is(err, failure) || len(b.extra) == 0 {
		t.Fatal("failed growth lost new owner", err)
	}
	if !bytes.Equal(before, b.Bytes()) {
		t.Fatal("failed growth modified logical image")
	}
	if err = b.Append(nil); !errors.Is(err, failure) {
		t.Fatal("poisoned builder reused", err)
	}
	if _, _, err = b.Take(); !errors.Is(err, failure) {
		t.Fatal("failed builder transferred", err)
	}
	if err = b.Close(); !errors.Is(err, failure) || b.mem == nil || b.extra == nil {
		t.Fatal("failed close lost owners", err)
	}
	base1, base2 := pointer(b.mem), pointer(b.extra)
	ops.unmap = unmap
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	if permissions(t, base1) != "" || permissions(t, base2) != "" {
		t.Fatal("growth cleanup retry leaked")
	}
}

func TestInvocationRetirementWaits(t *testing.T) {
	ops := testMemory()
	b, err := newCodeBuffer(4, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Append([]byte{0x13, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	p, err := publishImage(b, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	e, err := newEngine(MinNativeStackBytes, func(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64) {
		close(entered)
		<-release
		return 42, 0
	}, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	callDone := make(chan error, 1)
	go func() {
		got, err := e.Call(p, 0, 19, 23, 16)
		if got != 42 && err == nil {
			err = fmt.Errorf("result %d", got)
		}
		callDone <- err
	}()
	<-entered
	base := pointer(p.mem)
	closeDone := make(chan error, 1)
	go func() { closeDone <- p.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("retired active image: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	if permissions(t, base) == "" {
		t.Fatal("mapping retired during invocation")
	}
	close(release)
	if err := <-callDone; err != nil {
		t.Fatal(err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if _, err = e.Call(p, 0, 0, 0, 16); !errors.Is(err, ErrClosed) {
		t.Fatal("closed image reentered", err)
	}
	if permissions(t, base) != "" {
		t.Fatal("retired mapping remains")
	}
}

func TestEngineFailuresAndSerialization(t *testing.T) {
	failure := errors.New("injected trap map failure")
	ops := testMemory()
	mapRW, unmap := ops.mapRW, ops.unmap
	count := 0
	ops.mapRW = func(n int) ([]byte, error) {
		count++
		if count == 2 {
			return nil, failure
		}
		return mapRW(n)
	}
	ops.unmap = func([]byte) error { return failure }
	e, err := newEngine(MinNativeStackBytes, nativeCall, ops)
	if !errors.Is(err, failure) || e == nil || !e.closed {
		t.Fatal("failed engine lost cleanup owner", err)
	}
	base := pointer(e.stack)
	ops.unmap = unmap
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if permissions(t, base) != "" {
		t.Fatal("failed engine leaked stack")
	}
	for _, n := range []int{-1, 0, MinNativeStackBytes - 16, MinNativeStackBytes + 1, MaxNativeStackBytes + 16} {
		if _, err := newEngine(n, nativeCall, testMemory()); err == nil {
			t.Fatalf("stack capacity %d admitted", n)
		}
	}
	if !nativeAvailable {
		if _, err := NewEngine(MinNativeStackBytes); !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	}
	// Concurrent callers on one engine serialize before entering native code.
	ops = testMemory()
	b, _ := newCodeBuffer(4, 4, ops)
	if err = b.Append([]byte{0x13, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	p, err := publishImage(b, 4, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var invokeMu sync.Mutex
	e, err = newEngine(MinNativeStackBytes, func(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64) {
		if !invokeMu.TryLock() {
			panic("concurrent engine entry")
		}
		defer invokeMu.Unlock()
		return uint64(a0 + a1), 0
	}, ops)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				if got, err := e.Call(p, 0, 19, 23, 16); err != nil || got != 42 {
					t.Errorf("got=%d err=%v", got, err)
				}
			}
		}()
	}
	wg.Wait()
	for _, f := range []int{-16, 0, 1, 15, 17, MaxNativeStackBytes} {
		if _, err := e.Call(p, 0, 0, 0, f); !errors.Is(err, ErrStackBounds) {
			t.Fatal("frame", f, err)
		}
	}
	for _, off := range []int{-4, 1, 4} {
		if _, err := e.Call(p, off, 0, 0, 16); err == nil {
			t.Fatal("offset", off)
		}
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Call(p, 0, 0, 0, 16); !errors.Is(err, ErrClosed) {
		t.Fatal("closed engine reentered", err)
	}
}

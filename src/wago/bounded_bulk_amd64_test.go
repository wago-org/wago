//go:build linux && amd64 && !tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBoundedBulkRuntimeAdmission(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithOptimization("prepared-bounded-entry", enabled).Compile(boundedBulkFixture(64))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("performance")
		if err != nil {
			t.Fatal(err)
		}
		want := uint32(0)
		if enabled {
			want = 65536
		}
		if fn.boundedBulkLimit != want {
			t.Fatalf("enabled=%t limit=%d want=%d", enabled, fn.boundedBulkLimit, want)
		}
		for _, n := range []uint32{0, 1, 511, 512, 65535, 65536, 65537, 131072, 131073, 0xffffffff} {
			args := []uint64{uint64(n) | 0xaabbccdd00000000}
			if got := fn.boundedBulkArguments(args); got != (enabled && n <= 65536) {
				t.Fatalf("enabled=%t n=%d admitted=%t", enabled, n, got)
			}
			got, err := fn.Invoke(args...)
			valid := n > 0 && n <= 131072
			var trap *TrapError
			if valid {
				if err != nil || len(got) != 1 || got[0] != 896 {
					t.Fatalf("n=%d got=%v err=%v", n, got, err)
				}
			} else if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
				t.Fatalf("n=%d trap=%v", n, err)
			}
		}
		// Publication revokes isolated ownership; the same handle must preserve
		// ordinary admission and trapping behavior after a memory view escapes.
		mem := in.Memory().UnsafeBytes()
		for i := range mem {
			mem[i] = 0x55
		}
		wantMem := bytes.Clone(mem)
		for i := 0; i < 65536; i++ {
			wantMem[i] = 7
		}
		got, err := fn.Invoke(65536)
		if err != nil || len(got) != 1 || got[0] != 896 || !bytes.Equal(mem, wantMem) {
			t.Fatalf("published memory result=%v err=%v", got, err)
		}
		in.Close()
		c.Close()
	}
}

func TestBoundedBulkPrimitiveArguments(t *testing.T) {
	for _, copyOp := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(bulkPrimitiveFixture(3, 2, copyOp, nil))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("bulk")
		if err != nil {
			t.Fatal(err)
		}
		want := uint32(boundedBulkBytes)
		if copyOp {
			want /= 2
		}
		if fn.boundedBulkArgument != 3 || fn.boundedBulkLimit != want {
			t.Fatalf("copy=%t index=%d limit=%d", copyOp, fn.boundedBulkArgument, fn.boundedBulkLimit)
		}
		for _, n := range []uint32{0, 1, 511, 65536, want, want + 1, 0xffffffff} {
			args := []uint64{0xffffffff00000001, 0xaabbccdd00000007, uint64(n) | 0x8765432100000000}
			if got := fn.boundedBulkArguments(args); got != (n <= want) {
				t.Fatalf("copy=%t n=%d admitted=%t", copyOp, n, got)
			}
		}
		if _, err := fn.Invoke(1, 7, 511); err != nil {
			t.Fatal(err)
		}
		in.Close()
		c.Close()
	}
}

func TestBoundedBulkProofIsPrivateAndCodeNeutral(t *testing.T) {
	c, err := NewRuntimeConfig().Compile(boundedBulkFixture(64))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.boundedBulkPlans) != 2 || c.directPreparedBoundedAt(1) {
		t.Fatal("conditional work proof became unconditional")
	}
	before, err := compiledMetadataBytes(c, false)
	if err != nil {
		t.Fatal(err)
	}
	plan := c.boundedBulkPlans
	c.boundedBulkPlans = nil
	after, err := compiledMetadataBytes(c, false)
	if err != nil {
		t.Fatal(err)
	}
	c.boundedBulkPlans = plan
	if !bytes.Equal(before, after) {
		t.Fatal("metadata encoding retained the private capability")
	}
	plain, err := NewRuntimeConfig().WithOptimization("prepared-bounded-entry", false).Compile(boundedBulkFixture(64))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if !bytes.Equal(plain.code, c.code) {
		t.Fatal("entry budget changed generated guest code")
	}
	for _, workers := range []int{1, 4} {
		other, err := NewRuntimeConfig().WithFunctionWorkers(workers).Compile(boundedBulkFixture(64))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(other.boundedBulkPlans, c.boundedBulkPlans) {
			t.Fatalf("workers=%d plans=%x", workers, other.boundedBulkPlans)
		}
		other.Close()
	}
}

func TestBoundedBulkSchedulerAndGCProgress(t *testing.T) {
	prior := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prior)
	c, err := NewRuntimeConfig().Compile(boundedBulkFixture(64))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("performance")
	if err != nil {
		t.Fatal(err)
	}
	if fn.boundedBulkLimit == 0 {
		t.Fatal("missing work proof")
	}
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(start)
		for range 2000 {
			got, err := fn.Invoke(65536)
			if err != nil {
				done <- err
				return
			}
			if len(got) != 1 || got[0] != 896 {
				done <- fmt.Errorf("result %v", got)
				return
			}
		}
		done <- nil
	}()
	<-start
	gcDone := make(chan struct{})
	go func() {
		for range 10 {
			runtime.GC()
			runtime.Gosched()
		}
		close(gcDone)
	}()
	select {
	case <-gcDone:
	case <-time.After(5 * time.Second):
		t.Fatal("bounded native calls stalled GC")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bounded native calls did not finish")
	}
}

func TestBoundedBulkGuardTrapRecovery(t *testing.T) {
	for _, mode := range []BoundsCheckMode{BoundsChecksExplicit, BoundsChecksSignalsBased} {
		if mode == BoundsChecksSignalsBased && !guardPageBuilt {
			continue
		}
		c, err := NewRuntimeConfig().WithBoundsChecks(mode).Compile(boundedBulkFixture(64))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("performance")
		if err != nil {
			t.Fatal(err)
		}
		for range 5 {
			_, err = fn.Invoke(0)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
				t.Fatalf("mode=%v trap=%v", mode, err)
			}
			got, err := fn.Invoke(65536)
			if err != nil || len(got) != 1 || got[0] != 896 {
				t.Fatalf("mode=%v recovery=%v,%v", mode, got, err)
			}
		}
		in.Close()
		c.Close()
	}
}

func TestBoundedBulkStaticAndMaximumWork(t *testing.T) {
	constant := uint32(1024)
	c, err := NewRuntimeConfig().Compile(bulkPrimitiveFixture(0, 0, false, &constant))
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c)
	if err != nil {
		c.Close()
		t.Fatal(err)
	}
	fn, err := in.WasmFunc("bulk")
	if err != nil {
		t.Fatal(err)
	}
	if fn.boundedBulkArgument != 0 || !fn.boundedBulkArguments(nil) {
		t.Fatal("input-independent work was not admitted")
	}
	if got, err := fn.Invoke(); err != nil || len(got) != 0 {
		t.Fatalf("static result=%v err=%v", got, err)
	}
	in.Close()
	c.Close()

	source := bulkPrimitiveFixture(3, 2, false, nil)
	old := wasmtest.Section(5, wasmtest.Vec([]byte{1, 2, 0x80, 2}))
	memory := wasmtest.Section(5, wasmtest.Vec([]byte{1, 64, 0x80, 2}))
	if bytes.Count(source, old) != 1 {
		t.Fatal("missing primitive memory declaration")
	}
	source = bytes.Replace(source, old, memory, 1)
	c, err = NewRuntimeConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err = Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err = in.WasmFunc("bulk")
	if err != nil {
		t.Fatal(err)
	}
	args := []uint64{0, 0xdeadbeef, boundedBulkBytes}
	if !fn.boundedBulkArguments(args) {
		t.Fatal("maximum logical work was rejected")
	}
	if _, err = fn.Invoke(args...); err != nil {
		t.Fatal(err)
	}
	mem := in.Memory().UnsafeBytes()
	if uint64(len(mem)) != boundedBulkBytes || !bytes.Equal(mem, bytes.Repeat([]byte{0xef}, len(mem))) {
		t.Fatal("maximum fill changed the wrong bytes")
	}
	if fn.boundedBulkArguments([]uint64{0, 7, boundedBulkBytes + 1}) {
		t.Fatal("oversized invocation retained bounded scheduling")
	}
	_, err = fn.Invoke(0, 7, boundedBulkBytes+1)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds || !bytes.Equal(mem, bytes.Repeat([]byte{0xef}, len(mem))) {
		t.Fatalf("oversized fill err=%v", err)
	}
}

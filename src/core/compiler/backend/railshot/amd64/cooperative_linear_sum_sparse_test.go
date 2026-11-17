//go:build amd64 && linux && wago_experimental_cooperative_linear_sum

package amd64

import (
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// This qualification gate must run separately for useful process-wide VmHWM.
// MAP_NORESERVE is a virtual reservation, not a promised physical RSS bound.
func TestCooperativeLinearSumSparse4GiBQualification(t *testing.T) {
	logPeak := func(stage string) {
		data, err := os.ReadFile("/proc/self/status")
		if err != nil {
			t.Logf("%s VmHWM unavailable: %v", stage, err)
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VmHWM:") {
				t.Logf("%s %s", stage, line)
				return
			}
		}
		t.Logf("%s VmHWM unavailable in status", stage)
	}
	logPeak("before")
	defer logPeak("after")
	jm, err := coreruntime.NewJobMemory(1 << 32)
	if err != nil {
		t.Fatalf("UNQUALIFIED: sparse 4 GiB reservation failed: %v", err)
	}
	defer func() {
		if err := jm.Close(); err != nil {
			t.Errorf("close sparse reservation: %v", err)
		}
	}()
	mem := jm.CurrentBytes()
	if uint64(len(mem)) != uint64(1)<<32 {
		t.Fatalf("not a full memory32 extent: %d", len(mem))
	}
	// Exactly 64 bytes at each end, no clear/copy/iteration of the reservation.
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint64(mem[i*8:i*8+8], uint64(i+1)*0x9e3779b97f4a7c15)
		end := len(mem) - 64 + i*8
		binary.LittleEndian.PutUint64(mem[end:end+8], ^uint64(i)*0xd1342543de82ef95)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	args, results, trap := arena.Alloc(16), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
	type result struct {
		value  uint64
		bounds bool
	}
	reference := make(map[[2]uint32]result)
	// All successful cases read within the 64-byte boundary neighborhoods.
	starts := []uint32{0xfffffff0, 0xfffffff8, 0xfffffff9, 0xfffffffc}
	counts := []uint32{0, 1, 2, 3, 4, 5, 7, 8, 9}
	for _, enabled := range []bool{false, true} {
		m := linearSumWrappingModuleAMD64(t)
		cm, e := CompileModuleWith(m, CompileOptions{Interruptible: true, Optimizations: map[string]bool{"linear-sum-loop": enabled}})
		if e != nil {
			t.Fatal(e)
		}
		// No deferred accumulation of mappings across variants.
		func() {
			defer cm.CodeImage.Close()
			code, entry, e := coreruntime.MapCode(cm.Code)
			if e != nil {
				t.Fatal(e)
			}
			defer coreruntime.Unmap(code)
			for _, start := range starts {
				for _, n := range counts {
					clear(trap)
					clear(results)
					binary.LittleEndian.PutUint32(args, start)
					binary.LittleEndian.PutUint32(args[8:], n)
					e = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
					var te *coreruntime.TrapError
					got := result{value: binary.LittleEndian.Uint64(results)}
					if e != nil {
						if !errors.As(e, &te) || te.Code != coreruntime.TrapLinMemOutOfBounds {
							t.Fatalf("enabled=%v start=%x n=%d unexpected trap %v", enabled, start, n, e)
						}
						got.bounds = true
					}
					var want uint64
					wantBounds := false
					addr := start
					for i := uint32(0); i < n; i++ {
						if uint64(addr)+8 > uint64(1)<<32 {
							wantBounds = true
							break
						}
						want += binary.LittleEndian.Uint64(mem[uint64(addr) : uint64(addr)+8])
						addr += 8
					}
					if got.bounds != wantBounds || !wantBounds && got.value != want {
						t.Fatalf("enabled=%v start=%x n=%d got=%+v want=%x bounds=%v", enabled, start, n, got, want, wantBounds)
					}
					key := [2]uint32{start, n}
					if !enabled {
						reference[key] = got
					} else {
						old := reference[key]
						if old.bounds != got.bounds || !got.bounds && old.value != got.value {
							t.Fatalf("enabled/disabled mismatch %v", key)
						}
					}
				}
			}
		}()
	}
	logPeak("mapped-boundary-touched")
}

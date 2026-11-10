//go:build linux && amd64 && !tinygo

package wago

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The containing VMA can include adjacent owners; its RSS is an upper bound.
func logIdleMapping(t *testing.T, stage string, address uintptr) {
	t.Helper()
	f, err := os.Open("/proc/self/smaps")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	selected := false
	mapping := "unmapped"
	rss, pss, huge := 0, 0, 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if parts := strings.Split(fields[0], "-"); len(parts) == 2 {
			lo, e1 := strconv.ParseUint(parts[0], 16, 64)
			hi, e2 := strconv.ParseUint(parts[1], 16, 64)
			selected = e1 == nil && e2 == nil && uint64(address) >= lo && uint64(address) < hi
			if selected {
				mapping = scanner.Text()
			}
		} else if selected && len(fields) >= 2 {
			if fields[0] == "AnonHugePages:" {
				huge, err = strconv.Atoi(fields[1])
				if err != nil {
					t.Fatal(err)
				}
			}
			if fields[0] == "Rss:" {
				rss, err = strconv.Atoi(fields[1])
				if err != nil {
					t.Fatal(err)
				}
			}
			if fields[0] == "Pss:" {
				pss, err = strconv.Atoi(fields[1])
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	var use syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &use); err != nil {
		t.Fatal(err)
	}
	t.Logf("stage=%s owner-address=%x VMA=%q rss-KiB=%d pss-KiB=%d anon-huge-KiB=%d whole-process-maxrss-KiB=%d minor=%d major=%d HeapAlloc=%d HeapInuse=%d HeapReleased=%d StackInuse=%d NumGC=%d", stage, address, mapping, rss, pss, huge, use.Maxrss, use.Minflt, use.Majflt, mem.HeapAlloc, mem.HeapInuse, mem.HeapReleased, mem.StackInuse, mem.NumGC)
}

func TestNativeIdleCacheResidencyDiagnostic(t *testing.T) {
	if os.Getenv("WAGO_NATIVE_RESIDENCY_DIAGNOSTIC") != "1" {
		t.Skip("opt-in normal-operation mapping diagnostic")
	}
	for _, stackBytes := range []uint64{DefaultNativeStackBytes, 8 << 20} {
		t.Run(fmt.Sprintf("stack-%d", stackBytes), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithNativeStackBytes(stackBytes), boundedLargeFrameRecursionModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			address := in.eng.StackTop() - 1
			owner := in.eng
			logIdleMapping(t, "created", address)
			depth := uint64(45)
			if stackBytes > DefaultNativeStackBytes {
				depth = 120
			}
			if _, err := in.Invoke("recurse", depth); err != nil {
				t.Fatal(err)
			}
			logIdleMapping(t, "touched", address)
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
			logIdleMapping(t, "closed-idle", address)
			small, err := Compile(nil, benchAddOneModule())
			if err != nil {
				t.Fatal(err)
			}
			defer small.Close()
			next, err := Instantiate(small)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			t.Logf("same-engine-owner=%v", owner == next.eng)
			if _, err := next.Invoke("f", 1); err != nil {
				t.Fatal(err)
			}
			if err := next.Close(); err != nil {
				t.Fatal(err)
			}
			logIdleMapping(t, "small-closed-old-address", address)
		})
	}
	for _, count := range []uint32{10000, 100000} {
		t.Run(fmt.Sprintf("arena-table-%d", count), func(t *testing.T) {
			table := append([]byte{0x70, 1}, wasmtest.ULEB(count)...)
			table = append(table, wasmtest.ULEB(count)...)
			elements := append([]byte{0, 0x41, 0, 0x0b}, wasmtest.ULEB(count)...)
			elements = append(elements, make([]byte, count)...)
			c, err := Compile(nil, wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})),
				wasmtest.Section(3, []byte{1, 0}),
				wasmtest.Section(4, wasmtest.Vec(table)),
				wasmtest.Section(9, wasmtest.Vec(elements)),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
			))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			address, owner := in.nativeContext, in.ar
			t.Logf("arena-required-bytes=%d", c.executionView().instantiateArenaNeed)
			logIdleMapping(t, "initialized", address)
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
			logIdleMapping(t, "closed-idle", address)
			small, err := Compile(nil, benchAddOneModule())
			if err != nil {
				t.Fatal(err)
			}
			defer small.Close()
			next, err := Instantiate(small)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			t.Logf("same-arena-owner=%v", owner == next.ar)
			if _, err := next.Invoke("f", 1); err != nil {
				t.Fatal(err)
			}
			if err := next.Close(); err != nil {
				t.Fatal(err)
			}
			logIdleMapping(t, "small-closed-old-address", address)
		})
	}
}

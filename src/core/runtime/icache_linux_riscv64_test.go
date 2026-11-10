//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"errors"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRV64ProcessWideCacheSyscallAndFailure(t *testing.T) {
	calls := 0
	flush := func(number, start, end, flags uintptr) (uintptr, uintptr, syscall.Errno) {
		calls++
		if number != unix.SYS_RISCV_FLUSH_ICACHE || start != 0x10000 || end != 0x10030 || flags != 0 {
			t.Fatal("process-wide synchronization arguments", number, start, end, flags)
		}
		return 0, 0, syscall.EIO
	}
	if err := synchronizeExecutableRange(0x10000, 48, flush); !errors.Is(err, syscall.EIO) || calls != 1 {
		t.Fatal(err, calls)
	}
	for _, tc := range []struct {
		start  uintptr
		length int
	}{{0, 1}, {1, 0}, {1, -1}, {^uintptr(0) - 7, 8}} {
		if err := synchronizeExecutableRange(tc.start, tc.length, flush); err == nil || calls != 1 {
			t.Fatal("invalid range reached synchronization", tc, err, calls)
		}
	}
}

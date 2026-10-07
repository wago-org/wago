//go:build linux && amd64

package plugin

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

const testLinuxX32SyscallBit = uintptr(0x40000000)

func init() {
	if os.Getenv("WAGO_TEST_STAGED_RUNTIME_X32") != "1" {
		return
	}
	// x32 uses the x86-64 audit architecture but aliases syscall numbers with
	// this bit. The validation sandbox must reject the alias before dispatch.
	_, _, errno := syscall.RawSyscall(uintptr(syscall.SYS_FORK)|testLinuxX32SyscallBit, 0, 0, 0)
	if errno == syscall.EPERM {
		os.Exit(0)
	}
	_, _ = fmt.Fprintf(os.Stderr, "x32 fork alias was not contained: %v\n", errno)
	os.Exit(2)
}

func TestVerifyStagedRuntimeRejectsX32ProcessSyscallAlias(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_TEST_STAGED_RUNTIME_X32", "1")
	if err := verifyStagedRuntime(executable); err != nil {
		t.Fatalf("x32 process syscall alias escaped the validation filter: %v", err)
	}
}

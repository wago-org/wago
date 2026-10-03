//go:build linux

package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const stagedRuntimeHelperEnvironment = "WAGO_INTERNAL_STAGED_RUNTIME_SECCOMP_HELPER"

const linuxX32SyscallBit = uint32(0x40000000)

func init() {
	if os.Getenv(stagedRuntimeHelperEnvironment) != "1" {
		return
	}
	if len(os.Args) != 2 {
		_, _ = fmt.Fprintln(os.Stderr, "invalid staged runtime helper invocation")
		os.Exit(2)
	}
	target := os.Args[1]
	_ = os.Unsetenv(stagedRuntimeHelperEnvironment)
	// Seccomp is installed per OS thread. Pin this goroutine through Exec so the
	// staged image cannot migrate to an unfiltered runtime thread in between.
	runtime.LockOSThread()
	if err := prohibitStagedRuntimeProcesses(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "contain staged runtime: %v\n", err)
		os.Exit(2)
	}
	if err := syscall.Exec(target, []string{target}, os.Environ()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "execute staged runtime: %v\n", err)
		os.Exit(2)
	}
}

func stagedRuntimeCommand(ctx context.Context, binary string) (*exec.Cmd, error) {
	helper, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, helper, binary)
	command.Env = append(os.Environ(), stagedRuntimeHelperEnvironment+"=1")
	return command, nil
}

func prohibitStagedRuntimeProcesses() error {
	arch, denied, unavailable, ok := stagedRuntimeSeccompArchitecture()
	if !ok {
		return fmt.Errorf("process containment is unsupported on linux/%s", runtime.GOARCH)
	}
	filters := []unix.SockFilter{
		bpfStatement(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, 4),
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, arch, 1, 0),
		bpfStatement(unix.BPF_RET|unix.BPF_K, unix.SECCOMP_RET_KILL_PROCESS),
		bpfStatement(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, 0),
	}
	deny := uint32(unix.SECCOMP_RET_ERRNO) | uint32(unix.EPERM)
	missing := uint32(unix.SECCOMP_RET_ERRNO) | uint32(unix.ENOSYS)
	if arch == unix.AUDIT_ARCH_X86_64 {
		// x32 shares AUDIT_ARCH_X86_64 but sets bit 30 on syscall numbers.
		// Reject the alternate ABI before exact process-syscall comparisons.
		filters = append(filters,
			bpfJump(unix.BPF_JMP|unix.BPF_JSET|unix.BPF_K, linuxX32SyscallBit, 0, 1),
			bpfStatement(unix.BPF_RET|unix.BPF_K, deny),
		)
	}
	for _, number := range unavailable {
		filters = append(filters,
			bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, number, 0, 1),
			bpfStatement(unix.BPF_RET|unix.BPF_K, missing),
		)
	}
	for _, number := range denied {
		filters = append(filters,
			bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, number, 0, 1),
			bpfStatement(unix.BPF_RET|unix.BPF_K, deny),
		)
	}
	filters = append(filters,
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, uint32(unix.SYS_CLONE), 0, 3),
		bpfStatement(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, 16),
		bpfJump(unix.BPF_JMP|unix.BPF_JSET|unix.BPF_K, uint32(unix.CLONE_THREAD), 1, 0),
		bpfStatement(unix.BPF_RET|unix.BPF_K, deny),
		bpfStatement(unix.BPF_RET|unix.BPF_K, unix.SECCOMP_RET_ALLOW),
	)
	program := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0); err != nil {
		return err
	}
	return nil
}

func bpfStatement(code uint16, value uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: value}
}

func bpfJump(code uint16, value uint32, yes, no uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, K: value, Jt: yes, Jf: no}
}

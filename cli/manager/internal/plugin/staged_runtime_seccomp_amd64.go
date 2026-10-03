//go:build linux && amd64

package plugin

import "golang.org/x/sys/unix"

func stagedRuntimeSeccompArchitecture() (uint32, []uint32, []uint32, bool) {
	return unix.AUDIT_ARCH_X86_64, []uint32{unix.SYS_FORK, unix.SYS_VFORK}, []uint32{unix.SYS_CLONE3}, true
}

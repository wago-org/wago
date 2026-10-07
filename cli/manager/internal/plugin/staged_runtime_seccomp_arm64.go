//go:build linux && arm64

package plugin

import "golang.org/x/sys/unix"

func stagedRuntimeSeccompArchitecture() (uint32, []uint32, []uint32, bool) {
	return unix.AUDIT_ARCH_AARCH64, nil, []uint32{unix.SYS_CLONE3}, true
}

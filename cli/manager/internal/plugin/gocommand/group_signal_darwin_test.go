//go:build darwin

package gocommand

import (
	"errors"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinGroupSignalEPERMPreservesLiveMemberFailure(t *testing.T) {
	if err := normalizeGroupSignalError(syscall.Getpgrp(), syscall.EPERM); err != syscall.EPERM {
		t.Fatalf("live process group EPERM = %v, want genuine signal failure", err)
	}
	if !darwinGroupHasLiveMembers([]unix.KinfoProc{{Proc: unix.ExternProc{P_stat: 1}}}) {
		t.Fatal("live member must prevent EPERM normalization")
	}
	if darwinGroupHasLiveMembers([]unix.KinfoProc{{Proc: unix.ExternProc{P_stat: darwinZombieProcessState}}}) {
		t.Fatal("zombie-only group must permit EPERM normalization")
	}
	if err := normalizeGroupSignalError(syscall.Getpgrp(), syscall.EINVAL); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("non-EPERM signal error = %v, want original error", err)
	}
}

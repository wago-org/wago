//go:build darwin

package gocommand

import (
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

const darwinZombieProcessState = 5 // XNU's SZOMB

func normalizeGroupSignalError(pid int, err error) error {
	if !errors.Is(err, syscall.EPERM) {
		return err
	}
	// XNU returns EPERM when killpg finds only the unreaped zombie leader:
	// those members cannot receive a signal, and the group is already inert.
	// Inspect only this private process group on the exceptional EPERM path;
	// a live member (including a credential-changing child) or a failed query
	// must still fail the staged build instead of hiding a real kill failure.
	members, queryErr := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pid)
	if queryErr != nil {
		return errors.Join(err, fmt.Errorf("inspect Go process group %d: %w", pid, queryErr))
	}
	if darwinGroupHasLiveMembers(members) {
		return err
	}
	return nil
}

func darwinGroupHasLiveMembers(members []unix.KinfoProc) bool {
	for _, member := range members {
		if member.Proc.P_stat != darwinZombieProcessState {
			return true
		}
	}
	return false
}

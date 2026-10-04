//go:build darwin

package gocommand

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const darwinZombieProcessState = 5      // XNU's SZOMB
const darwinProcessExiting = 0x00002000 // XNU's public P_WEXIT

func normalizeGroupSignalError(pid int, err error) error {
	if !errors.Is(err, syscall.EPERM) {
		return err
	}
	// XNU returns EPERM when killpg finds only the unreaped zombie leader:
	// those members cannot receive a signal, and the group is already inert.
	// Inspect only this private process group on the exceptional EPERM path;
	// a live member (including a credential-changing child) or a failed query
	// must still fail the staged build instead of hiding a real kill failure.
	// NOTE_EXIT precedes SZOMB in XNU's proc_exit. During that transition
	// killpg can skip the exiting process while sysctl still reports it with
	// a non-zombie state and P_WEXIT. Wait briefly for an inert snapshot;
	// P_WEXIT alone is not proof that all process cleanup has finished.
	// The caller keeps the leader unreaped throughout this bounded wait.
	deadline := time.Now().Add(100 * time.Millisecond)
	return waitForDarwinGroupExit(pid, err, deadline, func() ([]unix.KinfoProc, error) {
		return unix.SysctlKinfoProcSlice("kern.proc.pgrp", pid)
	})
}

func waitForDarwinGroupExit(pid int, signalErr error, deadline time.Time, query func() ([]unix.KinfoProc, error)) error {
	for {
		if !time.Now().Before(deadline) {
			return signalErr
		}
		members, queryErr := query()
		if queryErr != nil {
			return errors.Join(signalErr, fmt.Errorf("inspect Go process group %d: %w", pid, queryErr))
		}
		if !time.Now().Before(deadline) {
			return signalErr
		}
		if !darwinGroupHasLiveMembers(members) {
			return nil
		}
		if darwinGroupHasRunningMembers(members) {
			return signalErr
		}
		time.Sleep(time.Millisecond)
	}
}

func darwinGroupHasLiveMembers(members []unix.KinfoProc) bool {
	for _, member := range members {
		if member.Proc.P_stat != darwinZombieProcessState {
			return true
		}
	}
	return false
}

func darwinGroupHasRunningMembers(members []unix.KinfoProc) bool {
	for _, member := range members {
		if member.Proc.P_stat != darwinZombieProcessState && member.Proc.P_flag&darwinProcessExiting == 0 {
			return true
		}
	}
	return false
}

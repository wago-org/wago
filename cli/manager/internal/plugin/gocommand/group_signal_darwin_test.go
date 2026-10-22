//go:build darwin

package gocommand

import (
	"errors"
	"syscall"
	"testing"
	"time"

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

func TestDarwinGroupSignalExitTransitionPolling(t *testing.T) {
	exiting := unix.KinfoProc{Proc: unix.ExternProc{P_stat: 2, P_flag: darwinProcessExiting}}
	zombie := unix.KinfoProc{Proc: unix.ExternProc{P_stat: darwinZombieProcessState}}
	running := unix.KinfoProc{Proc: unix.ExternProc{P_stat: 2}}
	for _, tc := range []struct {
		name     string
		members  []unix.KinfoProc
		queryErr error
		wantErr  error
	}{
		{name: "zombie", members: []unix.KinfoProc{zombie}},
		{name: "empty"},
		{name: "live descendant", members: []unix.KinfoProc{zombie, running}, wantErr: syscall.EPERM},
		{name: "query failure", queryErr: syscall.EIO, wantErr: syscall.EIO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := waitForDarwinGroupExit(123, syscall.EPERM, time.Now().Add(time.Minute), time.Now, func() ([]unix.KinfoProc, error) {
				calls++
				if calls == 1 {
					return []unix.KinfoProc{exiting}, nil
				}
				return tc.members, tc.queryErr
			})
			if calls != 2 || !errors.Is(err, tc.wantErr) {
				t.Fatalf("polling returned %v after %d queries, want %v after two queries", err, calls, tc.wantErr)
			}
			if tc.queryErr != nil && !errors.Is(err, syscall.EPERM) {
				t.Fatalf("inspection failure lost the original signal error: %v", err)
			}
		})
	}
}

func TestDarwinGroupSignalExitTransitionDeadline(t *testing.T) {
	// An already-expired budget cannot start another inspection.
	err := waitForDarwinGroupExit(123, syscall.EPERM, time.Now().Add(-time.Second), time.Now, func() ([]unix.KinfoProc, error) {
		t.Fatal("queried a group after the transition deadline")
		return nil, nil
	})
	if err != syscall.EPERM {
		t.Fatalf("expired transition returned %v, want EPERM", err)
	}
}

func TestDarwinGroupSignalSnapshotCrossingDeadline(t *testing.T) {
	exiting := unix.KinfoProc{Proc: unix.ExternProc{P_stat: 2, P_flag: darwinProcessExiting}}
	zombie := unix.KinfoProc{Proc: unix.ExternProc{P_stat: darwinZombieProcessState}}
	running := unix.KinfoProc{Proc: unix.ExternProc{P_stat: 2}}
	for _, tc := range []struct {
		name     string
		members  []unix.KinfoProc
		queryErr error
		wantErr  error
	}{
		{name: "empty"},
		{name: "zombie only", members: []unix.KinfoProc{zombie}},
		{name: "exiting", members: []unix.KinfoProc{exiting}, wantErr: syscall.EPERM},
		{name: "live descendant", members: []unix.KinfoProc{zombie, running}, wantErr: syscall.EPERM},
		{name: "exiting descendant", members: []unix.KinfoProc{zombie, exiting}, wantErr: syscall.EPERM},
		{name: "query failure", queryErr: syscall.EIO, wantErr: syscall.EIO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(123, 0)
			deadline := now.Add(100 * time.Millisecond)
			calls := 0
			err := waitForDarwinGroupExit(123, syscall.EPERM, deadline, func() time.Time { return now }, func() ([]unix.KinfoProc, error) {
				calls++
				// Model the caller resuming after its budget while sysctl
				// has already produced a snapshot. No real sleep is needed.
				now = deadline.Add(time.Millisecond)
				return tc.members, tc.queryErr
			})
			if calls != 1 || !errors.Is(err, tc.wantErr) {
				t.Fatalf("late snapshot returned %v after %d queries, want %v after one query", err, calls, tc.wantErr)
			}
			if tc.queryErr != nil && !errors.Is(err, syscall.EPERM) {
				t.Fatalf("inspection failure lost the original signal error: %v", err)
			}
		})
	}
}

func TestDarwinGroupSignalExitTransitionIsNotYetInert(t *testing.T) {
	exiting := unix.KinfoProc{Proc: unix.ExternProc{P_stat: 2, P_flag: darwinProcessExiting}}
	zombie := unix.KinfoProc{Proc: unix.ExternProc{P_stat: darwinZombieProcessState}}
	running := unix.KinfoProc{Proc: unix.ExternProc{P_stat: 2}}
	if !darwinGroupHasLiveMembers([]unix.KinfoProc{exiting, zombie}) {
		t.Fatal("P_WEXIT must not normalize EPERM before a zombie snapshot")
	}
	if darwinGroupHasRunningMembers([]unix.KinfoProc{exiting, zombie}) {
		t.Fatal("an exiting-only group should permit the bounded transition wait")
	}
	if !darwinGroupHasRunningMembers([]unix.KinfoProc{exiting, zombie, running}) {
		t.Fatal("a live descendant must preserve the signal failure immediately")
	}
	if darwinGroupHasLiveMembers([]unix.KinfoProc{zombie}) {
		t.Fatal("a completed zombie transition must permit EPERM normalization")
	}
}

//go:build windows

package run

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type watchedChildPlatform struct {
	job windows.Handle
}

func proxyWatchedInput() bool { return false }

func prepareWatchedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED}
}

func attachWatchedProcess(command *exec.Cmd) (watchedChildPlatform, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return watchedChildPlatform{}, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return watchedChildPlatform{}, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return watchedChildPlatform{}, err
	}
	err = windows.AssignProcessToJobObject(job, process)
	windows.CloseHandle(process)
	if err != nil {
		windows.CloseHandle(job)
		return watchedChildPlatform{}, err
	}
	if err := resumeWatchedProcess(command); err != nil {
		windows.CloseHandle(job)
		return watchedChildPlatform{}, err
	}
	return watchedChildPlatform{job: job}, nil
}

func resumeWatchedProcess(command *exec.Cmd) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return err
	}
	for {
		if entry.OwnerProcessID == uint32(command.Process.Pid) {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return err
			}
			count, resumeErr := windows.ResumeThread(thread)
			windows.CloseHandle(thread)
			if resumeErr != nil {
				return resumeErr
			}
			if count != 1 {
				return fmt.Errorf("watched process thread had suspend count %d, want 1", count)
			}
			return nil
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			return fmt.Errorf("find watched process thread: %w", err)
		}
	}
}

func interruptWatchedProcess(_ watchedChildPlatform, command *exec.Cmd, _ os.Signal) error {
	if command.Process == nil {
		return os.ErrProcessDone
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(command.Process.Pid))
}

func killWatchedProcess(platform watchedChildPlatform, _ *exec.Cmd) error {
	return windows.TerminateJobObject(platform.job, 1)
}

func releaseWatchedProcess(platform watchedChildPlatform, _ *exec.Cmd) {
	windows.CloseHandle(platform.job)
}

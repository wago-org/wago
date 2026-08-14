//go:build windows

package run

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type watchedChildPlatform struct {
	job windows.Handle
}

func prepareWatchedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
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
	return watchedChildPlatform{job: job}, nil
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

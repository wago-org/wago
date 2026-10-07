//go:build windows

package gocommand

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type commandTree struct {
	job       windows.Handle
	process   windows.Handle
	exitDone  chan struct{}
	exitErr   error
	signalMu  sync.Mutex
	assigned  bool
	signaled  bool
	signalErr error
}

func prepareCommandTree(command *exec.Cmd) (*commandTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	// Starting suspended closes the otherwise unavoidable gap between Start
	// and job assignment: the go process cannot spawn compiler children yet.
	if command.SysProcAttr == nil {
		command.SysProcAttr = new(syscall.SysProcAttr)
	}
	command.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	tree := &commandTree{job: job}
	command.Cancel = tree.signalJob
	return tree, nil
}

func (tree *commandTree) attach(_ context.Context, command *exec.Cmd) error {
	pid := uint32(command.Process.Pid)
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return fmt.Errorf("open suspended Go process: %w", err)
	}
	attached := false
	defer func() {
		if !attached {
			windows.CloseHandle(process)
		}
	}()
	threadID, err := primaryThreadID(pid)
	if err != nil {
		return err
	}
	thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, threadID)
	if err != nil {
		return fmt.Errorf("open suspended Go thread: %w", err)
	}
	defer windows.CloseHandle(thread)
	// Cmd.Cancel may run before attach. Keep assignment and resumption
	// serialized with signalJob, so an empty-job cancellation never consumes
	// the only signal intended for the newly assigned Go process.
	tree.signalMu.Lock()
	defer tree.signalMu.Unlock()
	if err := windows.AssignProcessToJobObject(tree.job, process); err != nil {
		return fmt.Errorf("assign Go process job: %w", err)
	}
	if _, err := windows.ResumeThread(thread); err != nil {
		return fmt.Errorf("resume Go process: %w", err)
	}
	tree.assigned = true
	tree.process = process
	tree.exitDone = make(chan struct{})
	attached = true
	go func() {
		// A direct Go leader can exit while a compiler child still holds
		// StdoutPipe. Observe its process handle without reaping it, then
		// terminate/wait for the job so streaming decoders can see EOF.
		status, err := windows.WaitForSingleObject(process, windows.INFINITE)
		if err == nil && status != windows.WAIT_OBJECT_0 {
			err = fmt.Errorf("Go process exit wait returned %d", status)
		}
		// Even an observation failure must fail closed by stopping the job.
		tree.exitErr = errors.Join(err, tree.terminateAndWait())
		close(tree.exitDone)
	}()
	return nil
}

func (tree *commandTree) beforeWait() error {
	<-tree.exitDone
	return tree.exitErr
}

func primaryThreadID(pid uint32) (uint32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err := windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID == pid {
			return entry.ThreadID, nil
		}
	}
	return 0, fmt.Errorf("find suspended Go thread for process %d", pid)
}

type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func (tree *commandTree) terminateAndWait() error {
	if err := tree.signalJob(); err != nil {
		return fmt.Errorf("terminate Go process job: %w", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var accounting jobAccounting
		if err := windows.QueryInformationJobObject(tree.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); err != nil {
			return fmt.Errorf("wait for Go process job: %w", err)
		}
		if accounting.ActiveProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Go process job still has %d active processes after termination", accounting.ActiveProcesses)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (tree *commandTree) signalJob() error {
	tree.signalMu.Lock()
	defer tree.signalMu.Unlock()
	if !tree.assigned {
		// Start checks ctx again immediately after attach and will signal
		// the populated job if cancellation arrived before this point.
		return nil
	}
	if !tree.signaled {
		tree.signalErr = windows.TerminateJobObject(tree.job, 1)
		tree.signaled = tree.signalErr == nil
	}
	return tree.signalErr
}

func (tree *commandTree) close() {
	if tree.process != 0 {
		_ = windows.CloseHandle(tree.process)
	}
	_ = windows.CloseHandle(tree.job)
}

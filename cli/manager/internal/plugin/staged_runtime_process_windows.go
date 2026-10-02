//go:build windows

package plugin

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	createRestrictedTokenDisableMaxPrivilege = uintptr(0x1)
	createRestrictedTokenWriteRestricted     = uintptr(0x8)
)

var createRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

// runBoundStagedRuntime places validation in a kill-on-close job. Closing the
// job on success, failure, or cancellation also terminates background children.
func runBoundStagedRuntime(command *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	// The job contains the trusted launcher and its one validation target. The
	// target receives a no-child token policy before it executes any plugin
	// initialization, while this limit remains defense in depth.
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
	limits.BasicLimitInformation.ActiveProcessLimit = 2
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	var closeOnce sync.Once
	closeJob := func() error {
		closed := false
		var closeErr error
		closeOnce.Do(func() {
			closed = true
			closeErr = windows.CloseHandle(job)
		})
		if !closed {
			return os.ErrProcessDone
		}
		return closeErr
	}
	restrictedToken, err := newStagedRuntimeRestrictedToken()
	if err != nil {
		_ = closeJob()
		return err
	}
	defer restrictedToken.Close()
	// CREATE_SUSPENDED closes the Start-to-Assign gap. The write-restricted
	// token also prevents the eventual target from borrowing PROCESS_CREATE_PROCESS,
	// PROCESS_VM_WRITE, or PROCESS_DUP_HANDLE rights from an out-of-job process;
	// Microsoft's child-process policy is not secure without that isolation.
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED,
		Token:         syscall.Token(restrictedToken),
	}
	command.Cancel = closeJob
	if err := command.Start(); err != nil {
		_ = closeJob()
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		_ = windows.CloseHandle(process)
	}
	if err != nil {
		_ = command.Process.Kill()
		waitErr := command.Wait()
		_ = closeJob()
		return errors.Join(err, waitErr)
	}
	if err := resumeStagedRuntime(uint32(command.Process.Pid)); err != nil {
		_ = closeJob()
		_ = command.Process.Kill()
		waitErr := command.Wait()
		return errors.Join(err, waitErr)
	}
	waitErr := command.Wait()
	closeErr := closeJob()
	if errors.Is(closeErr, os.ErrProcessDone) {
		closeErr = nil
	}
	return errors.Join(waitErr, closeErr)
}

func newStagedRuntimeRestrictedToken() (windows.Token, error) {
	var source windows.Token
	const access = windows.TOKEN_ASSIGN_PRIMARY | windows.TOKEN_DUPLICATE | windows.TOKEN_QUERY |
		windows.TOKEN_ADJUST_DEFAULT | windows.TOKEN_ADJUST_SESSIONID
	if err := windows.OpenProcessToken(windows.CurrentProcess(), access, &source); err != nil {
		return 0, fmt.Errorf("open validation source token: %w", err)
	}
	defer source.Close()
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return 0, fmt.Errorf("create validation restriction sid: %w", err)
	}
	// A fresh, ungranted SID makes write access checks fail for every external
	// process object while WRITE_RESTRICTED leaves executable and DLL reads alone.
	sid, err := windows.StringToSid(fmt.Sprintf("S-1-5-21-%d-%d-%d-%d",
		binary.LittleEndian.Uint32(entropy[0:4]), binary.LittleEndian.Uint32(entropy[4:8]),
		binary.LittleEndian.Uint32(entropy[8:12]), binary.LittleEndian.Uint32(entropy[12:16])))
	if err != nil {
		return 0, fmt.Errorf("create validation restriction sid: %w", err)
	}
	restriction := windows.SIDAndAttributes{Sid: sid}
	var token windows.Token
	success, _, callErr := createRestrictedToken.Call(
		uintptr(source), createRestrictedTokenDisableMaxPrivilege|createRestrictedTokenWriteRestricted,
		0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&restriction)), uintptr(unsafe.Pointer(&token)),
	)
	if success == 0 {
		if callErr == windows.ERROR_SUCCESS {
			callErr = syscall.EINVAL
		}
		return 0, fmt.Errorf("create validation restricted token: %w", callErr)
	}
	return token, nil
}

func resumeStagedRuntime(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot suspended validation threads: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if openErr != nil {
			return fmt.Errorf("open suspended validation thread: %w", openErr)
		}
		previous, resumeErr := windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		if resumeErr != nil {
			return fmt.Errorf("resume contained validation process: %w", resumeErr)
		}
		if previous == 0 {
			return errors.New("validation process was not suspended before job assignment")
		}
		return nil
	}
	if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return errors.New("suspended validation process has no primary thread")
	}
	return fmt.Errorf("enumerate suspended validation threads: %w", err)
}

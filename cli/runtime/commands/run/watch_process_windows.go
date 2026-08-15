//go:build windows && !wago_lean

package run

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type watchedChildPlatform struct {
	job windows.Handle
}

func startWatchedProcess(command *exec.Cmd) (watchedChildPlatform, error) {
	job, err := createWatchedJob()
	if err != nil {
		return watchedChildPlatform{}, err
	}
	platform := watchedChildPlatform{job: job}
	if err := createWatchedProcess(command, job); err != nil {
		windows.CloseHandle(job)
		return watchedChildPlatform{}, err
	}
	return platform, nil
}

func createWatchedJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func createWatchedProcess(command *exec.Cmd, job windows.Handle) error {
	stdin, closeStdin, err := watchedWindowsStream(command.Stdin, false)
	if err != nil {
		return err
	}
	if closeStdin {
		defer stdin.Close()
	}
	stdout, closeStdout, err := watchedWindowsStream(command.Stdout, true)
	if err != nil {
		return err
	}
	if closeStdout {
		defer stdout.Close()
	}
	stderr, closeStderr, err := watchedWindowsStream(command.Stderr, true)
	if err != nil {
		return err
	}
	if closeStderr {
		defer stderr.Close()
	}

	handles := [3]windows.Handle{}
	files := [3]*os.File{stdin, stdout, stderr}
	current := windows.CurrentProcess()
	for index, file := range files {
		if err := windows.DuplicateHandle(current, windows.Handle(file.Fd()), current, &handles[index], 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			closeWatchedWindowsHandles(handles[:])
			return err
		}
	}
	defer closeWatchedWindowsHandles(handles[:])

	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), unsafe.Sizeof(handles)); err != nil {
		return err
	}
	executable, err := windows.UTF16PtrFromString(command.Path)
	if err != nil {
		return err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(command.Args))
	if err != nil {
		return err
	}
	environment, err := watchedWindowsEnvironment(command.Environ())
	if err != nil {
		return err
	}
	var directory *uint16
	if command.Dir != "" {
		directory, err = windows.UTF16PtrFromString(command.Dir)
		if err != nil {
			return err
		}
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  handles[0],
			StdOutput: handles[1],
			StdErr:    handles[2],
		},
		ProcThreadAttributeList: attributes.List(),
	}
	info := windows.ProcessInformation{}
	err = windows.CreateProcess(
		executable,
		commandLine,
		nil,
		nil,
		true,
		windows.CREATE_DEFAULT_ERROR_MODE|windows.CREATE_NEW_PROCESS_GROUP|windows.CREATE_SUSPENDED|windows.CREATE_UNICODE_ENVIRONMENT|windows.EXTENDED_STARTUPINFO_PRESENT,
		&environment[0],
		directory,
		&startup.StartupInfo,
		&info,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(info.Thread)
	defer windows.CloseHandle(info.Process)
	if err := windows.AssignProcessToJobObject(job, info.Process); err != nil {
		_ = windows.TerminateProcess(info.Process, 1)
		return err
	}
	if _, err := windows.ResumeThread(info.Thread); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		return err
	}
	process, err := os.FindProcess(int(info.ProcessId))
	if err != nil {
		_ = windows.TerminateJobObject(job, 1)
		return err
	}
	command.Process = process
	return nil
}

func watchedWindowsStream(stream any, write bool) (*os.File, bool, error) {
	if file, ok := stream.(*os.File); ok {
		return file, false, nil
	}
	if stream != nil && stream != io.Discard {
		return nil, false, errors.New("Windows watch standard streams must be files")
	}
	flag := os.O_RDONLY
	if write {
		flag = os.O_WRONLY
	}
	file, err := os.OpenFile("NUL", flag, 0)
	return file, true, err
}

func watchedWindowsEnvironment(entries []string) ([]uint16, error) {
	seen := make(map[string]bool, len(entries))
	unique := make([]string, 0, len(entries))
	for index := len(entries) - 1; index >= 0; index-- {
		key := watchedWindowsEnvironmentKey(entries[index])
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, entries[index])
	}
	sort.Slice(unique, func(left, right int) bool {
		return strings.ToUpper(unique[left]) < strings.ToUpper(unique[right])
	})
	block := make([]uint16, 0, len(unique)*32)
	for _, entry := range unique {
		encoded, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, err
		}
		block = append(block, encoded...)
	}
	return append(block, 0), nil
}

func watchedWindowsEnvironmentKey(entry string) string {
	start := 0
	if strings.HasPrefix(entry, "=") {
		start = 1
	}
	separator := strings.IndexByte(entry[start:], '=')
	if separator < 0 {
		return strings.ToUpper(entry)
	}
	return strings.ToUpper(entry[:start+separator])
}

func closeWatchedWindowsHandles(handles []windows.Handle) {
	for _, handle := range handles {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
	}
}

func interruptWatchedProcess(_ watchedChildPlatform, command *exec.Cmd, _ os.Signal) (bool, error) {
	if command.Process == nil {
		return true, nil
	}
	return false, watchedWindowsInterruptError(windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(command.Process.Pid)))
}

func watchedWindowsInterruptError(err error) error {
	if errors.Is(err, windows.ERROR_INVALID_HANDLE) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return errWatchedGracefulStopUnavailable
	}
	return err
}

func killWatchedProcess(platform watchedChildPlatform, _ *exec.Cmd) error {
	return windows.TerminateJobObject(platform.job, 1)
}

func releaseWatchedProcess(platform watchedChildPlatform, _ *exec.Cmd) error {
	return windows.CloseHandle(platform.job)
}

func waitWatchedProcess(_ watchedChildPlatform, command *exec.Cmd) watchedProcessResult {
	err := command.Wait()
	result := watchedProcessResult{err: err}
	if command.ProcessState != nil {
		result.exitCode = command.ProcessState.ExitCode()
	}
	return result
}

func watchedStopError(watchedProcessResult, os.Signal, bool) error { return nil }

func continueWatchedProcess(watchedChildPlatform, *exec.Cmd) error { return nil }

func writeWatchedOutput(writer io.Writer, format string, arguments ...any) {
	_, _ = fmt.Fprintf(writer, format, arguments...)
}

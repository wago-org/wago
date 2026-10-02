//go:build windows

package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const stagedRuntimeWindowsHelperEnvironment = "WAGO_INTERNAL_STAGED_RUNTIME_WINDOWS_HELPER"

const (
	procThreadAttributeChildProcessPolicy = uintptr(0x0002000e)
	processCreationChildProcessRestricted = uint32(0x1)
	stagedRuntimeStillActive              = uint32(259)
)

func init() {
	if os.Getenv(stagedRuntimeWindowsHelperEnvironment) != "1" {
		return
	}
	if len(os.Args) != 2 {
		_, _ = fmt.Fprintln(os.Stderr, "invalid staged runtime helper invocation")
		os.Exit(2)
	}
	_ = os.Unsetenv(stagedRuntimeWindowsHelperEnvironment)
	exitCode, err := runWindowsStagedRuntimeTarget(os.Args[1])
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "contain staged runtime: %v\n", err)
		os.Exit(2)
	}
	os.Exit(int(exitCode))
}

func stagedRuntimeCommand(ctx context.Context, binary string) (*exec.Cmd, error) {
	helper, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, helper, binary)
	command.Env = append(os.Environ(), stagedRuntimeWindowsHelperEnvironment+"=1")
	return command, nil
}

func runWindowsStagedRuntimeTarget(binary string) (uint32, error) {
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, err
	}
	defer attributes.Delete()
	// The policy is installed by CreateProcess before imported plugin packages
	// initialize. Installing it in generated main would be too late because Go
	// initializes imports before entering that package's code.
	childPolicy := processCreationChildProcessRestricted
	if err := attributes.Update(procThreadAttributeChildProcessPolicy,
		unsafe.Pointer(&childPolicy), unsafe.Sizeof(childPolicy)); err != nil {
		return 0, err
	}
	application, err := windows.UTF16PtrFromString(binary)
	if err != nil {
		return 0, err
	}
	commandLine, err := windows.UTF16FromString(syscall.EscapeArg(binary))
	if err != nil {
		return 0, err
	}
	stdin, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return 0, err
	}
	stdout, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return 0, err
	}
	stderr, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if err != nil {
		return 0, err
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  stdin,
			StdOutput: stdout,
			StdErr:    stderr,
		},
		ProcThreadAttributeList: attributes.List(),
	}
	var process windows.ProcessInformation
	if err := windows.CreateProcess(application, &commandLine[0], nil, nil, true,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		nil, nil, &startup.StartupInfo, &process); err != nil {
		return 0, err
	}
	_ = windows.CloseHandle(process.Thread)
	defer windows.CloseHandle(process.Process)
	wait, err := windows.WaitForSingleObject(process.Process, windows.INFINITE)
	if err != nil {
		return 0, err
	}
	if wait != windows.WAIT_OBJECT_0 {
		return 0, fmt.Errorf("wait for staged runtime: unexpected result %#x", wait)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process.Process, &exitCode); err != nil {
		return 0, err
	}
	if exitCode == stagedRuntimeStillActive {
		return 0, errors.New("staged runtime remained active after wait")
	}
	return exitCode, nil
}

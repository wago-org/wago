//go:build windows

package plugin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsParentSpoofMode = "parent-spoof"
	windowsAlternateParent = "alternate-parent"
	windowsManagerPIDEnv   = "WAGO_TEST_MANAGER_PID"
)

func init() {
	mode := os.Getenv("WAGO_TEST_STAGED_RUNTIME")
	if mode == windowsAlternateParent {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	if mode != windowsParentSpoofMode {
		return
	}
	managerPID, err := strconv.ParseUint(os.Getenv(windowsManagerPIDEnv), 10, 32)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "parse manager pid: %v\n", err)
		os.Exit(2)
	}
	manager, err := windows.OpenProcess(windows.PROCESS_CREATE_PROCESS|windows.PROCESS_DUP_HANDLE, false, uint32(managerPID))
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// A contained validation token must not be able to borrow an
		// out-of-job process as the effective parent of an escaped child.
		os.Exit(0)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "open manager as alternate parent: %v\n", err)
		os.Exit(2)
	}
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), "WAGO_TEST_STAGED_RUNTIME=clean")
	child.SysProcAttr = &syscall.SysProcAttr{ParentProcess: syscall.Handle(manager)}
	err = child.Start()
	_ = windows.CloseHandle(manager)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_CHILD_PROCESS_BLOCKED) {
		os.Exit(0)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "start alternate-parent child: %v\n", err)
		os.Exit(2)
	}
	_ = child.Process.Release()
	// A successful start proves the child inherited the out-of-job manager's
	// accounting. Report it through the validator's exit status so the test
	// remains sound even when its final restricted token cannot write a marker.
	os.Exit(42)
}

func TestVerifyStagedRuntimeRejectsWindowsParentSpoofEscape(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Go duplicates the standard handles into an alternate parent before
	// CreateProcess, so grant both parent-creation and handle-duplication rights.
	// The restricted validator must still fail the second SID access check.
	descriptor, err := windows.SecurityDescriptorFromString("D:(A;;0x00c0;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	attributes := syscall.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(syscall.SecurityAttributes{})),
		SecurityDescriptor: uintptr(unsafe.Pointer(descriptor)),
	}
	alternate := exec.Command(executable)
	alternate.Env = append(os.Environ(), "WAGO_TEST_STAGED_RUNTIME="+windowsAlternateParent)
	alternate.SysProcAttr = &syscall.SysProcAttr{ProcessAttributes: &attributes}
	if err := alternate.Start(); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(descriptor)
	t.Cleanup(func() {
		_ = alternate.Process.Kill()
		_ = alternate.Wait()
	})
	parent, err := windows.OpenProcess(windows.PROCESS_CREATE_PROCESS|windows.PROCESS_DUP_HANDLE, false, uint32(alternate.Process.Pid))
	if err != nil {
		t.Fatalf("open permissive alternate parent for control: %v", err)
	}
	control := exec.Command(executable)
	control.Env = append(os.Environ(), "WAGO_TEST_STAGED_RUNTIME=clean")
	control.SysProcAttr = &syscall.SysProcAttr{ParentProcess: syscall.Handle(parent)}
	err = control.Run()
	_ = windows.CloseHandle(parent)
	if err != nil {
		t.Fatalf("alternate-parent control process: %v", err)
	}
	t.Setenv("WAGO_TEST_STAGED_RUNTIME", windowsParentSpoofMode)
	t.Setenv(windowsManagerPIDEnv, strconv.Itoa(alternate.Process.Pid))
	if err := verifyStagedRuntime(executable); err != nil {
		t.Fatalf("verify parent-spoof containment: %v", err)
	}
}

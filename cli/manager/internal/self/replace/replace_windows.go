//go:build windows

package replace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/wago-org/wago/internal/atomicfile"
)

const (
	moveFileReplaceExisting  = 0x1
	moveFileDelayUntilReboot = 0x4
	createNewProcessGroup    = 0x00000200
	createNoWindow           = 0x08000000
)

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func StageRemoval(executable string, targets []string) (string, error) {
	contained := false
	for _, target := range targets {
		if samePath(target, executable) {
			continue
		}
		if containsPath(target, executable) {
			contained = true
			break
		}
	}
	if !contained {
		return executable, nil
	}

	stagedFile, err := os.CreateTemp("", "wago-uninstall-*.exe")
	if err != nil {
		return "", err
	}
	staged := stagedFile.Name()
	if err := stagedFile.Close(); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	if err := os.Remove(staged); err != nil {
		return "", err
	}
	for _, target := range targets {
		if containsPath(target, staged) {
			return "", fmt.Errorf("temporary executable %s is inside cleanup target %s", staged, target)
		}
	}
	if err := os.Rename(executable, staged); err != nil {
		return "", err
	}
	return staged, nil
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func containsPath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func Executable(executable, staged string) (bool, error) {
	if err := atomicfile.ReplaceExisting(staged, executable); err == nil {
		return false, nil
	}
	return true, scheduleMove(staged, executable, moveFileReplaceExisting|moveFileDelayUntilReboot)
}

func Remove(executable string) (bool, error) {
	if err := os.Remove(executable); err == nil || os.IsNotExist(err) {
		return false, nil
	}
	return true, scheduleMove(executable, "", moveFileDelayUntilReboot)
}

// ScheduleTargetRemoval starts a detached PowerShell process that waits for
// this manager to exit before deleting its containing Wago home.
func ScheduleTargetRemoval(executable string, targets []string) (bool, error) {
	running, err := os.Executable()
	if err != nil {
		return false, err
	}
	runningInfo, err := os.Stat(running)
	if err != nil {
		return false, err
	}
	executableInfo, err := os.Stat(executable)
	if err != nil {
		return false, err
	}
	if !os.SameFile(runningInfo, executableInfo) {
		return false, nil
	}

	contained := false
	for _, target := range targets {
		if !samePath(target, executable) && containsPath(target, executable) {
			contained = true
			break
		}
	}
	if !contained {
		return false, nil
	}
	script, err := os.CreateTemp("", "wago-uninstall-*.ps1")
	if err != nil {
		return false, err
	}
	scriptPath := script.Name()
	if _, err := script.WriteString(targetRemovalScript(os.Getpid(), targets)); err != nil {
		_ = script.Close()
		_ = os.Remove(scriptPath)
		return false, err
	}
	if err := script.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return false, err
	}
	process := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	process.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGroup}
	if err := process.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return false, err
	}
	_ = process.Process.Release()
	return true, nil
}

func targetRemovalScript(parentPID int, targets []string) string {
	var script strings.Builder
	script.WriteString("$ErrorActionPreference = 'SilentlyContinue'\r\n")
	fmt.Fprintf(&script, "Wait-Process -Id %d\r\n", parentPID)
	for _, target := range targets {
		target = strings.ReplaceAll(target, "'", "''")
		fmt.Fprintf(&script, "for ($attempt = 0; $attempt -lt 20 -and (Test-Path -LiteralPath '%s'); $attempt++) {\r\n", target)
		fmt.Fprintf(&script, "  Remove-Item -LiteralPath '%s' -Recurse -Force\r\n", target)
		script.WriteString("  if (Test-Path -LiteralPath '" + target + "') { Start-Sleep -Milliseconds 250 }\r\n")
		script.WriteString("}\r\n")
	}
	script.WriteString("Remove-Item -LiteralPath $PSCommandPath -Force\r\n")
	return script.String()
}

func scheduleMove(source, destination string, flags uintptr) error {
	sourcePtr, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	var destinationPtr *uint16
	if destination != "" {
		destinationPtr, err = syscall.UTF16PtrFromString(destination)
		if err != nil {
			return err
		}
	}
	result, _, callErr := moveFileEx.Call(
		uintptr(unsafe.Pointer(sourcePtr)),
		uintptr(unsafe.Pointer(destinationPtr)),
		flags,
	)
	if result == 0 {
		return callErr
	}
	return nil
}

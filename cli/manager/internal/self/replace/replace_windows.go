//go:build windows

package replace

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/managedrelease"
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
		if samePath(target, executable) || containsPath(target, executable) {
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
// this manager to exit before deleting its containing Wago home or release.
// The running payload may differ from the stable launcher passed by the caller.
// The caller holds the publication lock until this function returns.
func ScheduleTargetRemoval(_ string, targets []string, lockPath string, emptyDirs []string) (bool, error) {
	running, err := os.Executable()
	if err != nil {
		return false, err
	}
	contained := false
	for _, target := range targets {
		if samePath(target, running) || containsPath(target, running) {
			contained = true
			break
		}
	}
	if !contained {
		return false, nil
	}
	// Capture the held coordinator so a delayed worker cannot delete a later
	// installation that has already retired and replaced this lock.
	file, err := os.Open(lockPath)
	if err != nil {
		return false, err
	}
	var identity syscall.ByHandleFileInformation
	err = syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &identity)
	file.Close()
	if err != nil {
		return false, err
	}
	script, err := os.CreateTemp("", "wago-uninstall-*.ps1")
	if err != nil {
		return false, err
	}
	scriptPath := script.Name()
	if _, err := script.WriteString(targetRemovalScript(os.Getpid(), targets, lockPath, emptyDirs, identity)); err != nil {
		_ = script.Close()
		_ = os.Remove(scriptPath)
		return false, err
	}
	if err := script.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return false, err
	}
	undoPending, err := managedrelease.MarkUninstallPending(lockPath)
	if err != nil {
		os.Remove(scriptPath)
		return false, err
	}
	process := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	process.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGroup}
	if err := process.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return false, errors.Join(err, undoPending())
	}
	_ = process.Process.Release()
	return true, nil
}

func targetRemovalScript(parentPID int, targets []string, lockPath string, emptyDirs []string, identity syscall.ByHandleFileInformation) string {
	quote := func(path string) string { return "'" + strings.ReplaceAll(path, "'", "''") + "'" }
	var script strings.Builder
	script.WriteString("$ErrorActionPreference = 'Stop'\r\n")
	fmt.Fprintf(&script, "$lockPath = %s\r\n", quote(lockPath))
	fmt.Fprintf(&script, "$pendingPath = %s\r\n", quote(managedrelease.UninstallPendingPath(lockPath)))
	fmt.Fprintf(&script, "$expectedVolume = [uint32]%d; $expectedHigh = [uint32]%d; $expectedLow = [uint32]%d\r\n", identity.VolumeSerialNumber, identity.FileIndexHigh, identity.FileIndexLow)
	script.WriteString(`Add-Type @'
using System;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using Microsoft.Win32.SafeHandles;
public static class WagoLockIdentity {
    [StructLayout(LayoutKind.Sequential)]
    public struct Info {
        public uint Attributes, CreationLow, CreationHigh, AccessLow, AccessHigh;
        public uint WriteLow, WriteHigh, Volume, SizeHigh, SizeLow, Links, IndexHigh, IndexLow;
    }
    [DllImport("kernel32.dll", SetLastError=true)]
    static extern bool GetFileInformationByHandle(SafeFileHandle handle, out Info info);
    [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    static extern uint GetLongPathName(string path, StringBuilder result, uint size);
    public static string LongPath(string path) {
        var result = new StringBuilder(32768);
        uint size = GetLongPathName(path, result, (uint)result.Capacity);
        if (size == 0 || size >= result.Capacity) throw new System.ComponentModel.Win32Exception();
        return result.ToString();
    }
    public static bool Same(FileStream left, FileStream right, uint volume, uint high, uint low) {
        Info a, b;
        if (!GetFileInformationByHandle(left.SafeFileHandle, out a) ||
            !GetFileInformationByHandle(right.SafeFileHandle, out b))
            throw new System.ComponentModel.Win32Exception();
        return a.Volume == volume && a.IndexHigh == high && a.IndexLow == low &&
            a.Volume == b.Volume && a.IndexHigh == b.IndexHigh && a.IndexLow == b.IndexLow;
    }
}
'@
$share = [IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete
# Open the existing coordinator: a retired uninstall must not create a new one.
$lock = [IO.File]::Open($lockPath, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, $share)
$held = $false
try {
    for ($attempt = 0; $attempt -lt 240; $attempt++) {
        try { $lock.Lock(0, 1); $held = $true; break }
        catch [IO.IOException] { Start-Sleep -Milliseconds 250 }
    }
    if (!$held) { throw 'Timed out waiting for Wago publication lock' }
    $lockPath = [WagoLockIdentity]::LongPath($lockPath)
    $linked = [IO.File]::Open($lockPath, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, $share)
    try {
        if (![WagoLockIdentity]::Same($lock, $linked, $expectedVolume, $expectedHigh, $expectedLow)) { throw 'Wago publication lock was retired' }
    } finally { $linked.Dispose() }
    $pendingPath = [WagoLockIdentity]::LongPath($pendingPath)
`)
	fmt.Fprintf(&script, "    Wait-Process -Id %d -ErrorAction SilentlyContinue\r\n", parentPID)
	script.WriteString(`    function Remove-WagoTarget([string] $path) {
        if (!(Test-Path -LiteralPath $path)) { return }
        $path = [WagoLockIdentity]::LongPath($path)
        if ($path.Equals($lockPath, [StringComparison]::OrdinalIgnoreCase) -or $path.Equals($pendingPath, [StringComparison]::OrdinalIgnoreCase)) { return }
        $prefix = $path.TrimEnd([char]'\') + '\'
        if ($lockPath.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
            if (Test-Path -LiteralPath $path) {
                Get-ChildItem -LiteralPath $path -Force | ForEach-Object { Remove-WagoTarget $_.FullName }
            }
            return
        }
        for ($attempt = 0; $attempt -lt 20 -and (Test-Path -LiteralPath $path); $attempt++) {
            Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
            if (Test-Path -LiteralPath $path) { Start-Sleep -Milliseconds 250 }
        }
        if (Test-Path -LiteralPath $path) { throw "Cannot remove Wago target $path" }
    }
`)
	for _, target := range targets {
		fmt.Fprintf(&script, "    Remove-WagoTarget %s\r\n", quote(filepath.Clean(target)))
	}
	script.WriteString(`    [IO.File]::Delete($pendingPath)
    # No recursive deletion is allowed after this retirement point.
    $retired = Join-Path ([IO.Path]::GetDirectoryName($lockPath)) ('.retired-lock-' + [IO.Path]::GetRandomFileName())
    [IO.File]::Move($lockPath, $retired)
    [IO.File]::Delete($retired)
} finally {
    if ($held) { $lock.Unlock(0, 1) }
    $lock.Dispose()
}
`)
	for _, dir := range emptyDirs {
		fmt.Fprintf(&script, "try { [IO.Directory]::Delete(%s, $false) } catch [IO.IOException] {}\r\n", quote(dir))
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

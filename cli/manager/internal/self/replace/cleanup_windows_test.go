//go:build windows

package replace

import (
	"context"
	"github.com/wago-org/wago/internal/filelock"
	"github.com/wago-org/wago/internal/managedrelease"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTargetRemovalScriptWaitsThenRemovesTargets(t *testing.T) {
	script := targetRemovalScript(1234, []string{`C:\Users\A'lice\.wago`, `C:\Users\A'lice\.local\bin\wago.exe`}, `C:\Users\A'lice\.local\bin\.wago-release.lock`, nil, syscall.ByHandleFileInformation{})
	if strings.Index(script, "Wait-Process") < strings.Index(script, "$lock.Lock(0, 1)") {
		t.Fatal("cleanup waits for the parent before taking ownership")
	}
	for _, want := range []string{
		`Wait-Process -Id 1234`,
		`Remove-WagoTarget 'C:\Users\A''lice\.wago'`,
		`Remove-WagoTarget 'C:\Users\A''lice\.local\bin\wago.exe'`,
		`Start-Sleep -Milliseconds 250`,
		`Remove-Item -LiteralPath $PSCommandPath -Force`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("cleanup script missing %q:\n%s", want, script)
		}
	}
}

func TestScheduleTargetRemovalDeletesContainingDirectoryAfterExit(t *testing.T) {
	if os.Getenv("WAGO_TEST_DEFERRED_CLEANUP_CHILD") == "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		deferred, err := ScheduleTargetRemoval(executable, []string{filepath.Dir(executable)}, filepath.Join(filepath.Dir(executable), ".wago-release.lock"), []string{filepath.Dir(executable)})
		if err != nil {
			t.Fatal(err)
		}
		if !deferred {
			t.Fatal("ScheduleTargetRemoval did not defer cleanup")
		}
		return
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".wago-release.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "wago-self-uninstall-test.exe")
	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, data, 0o755); err != nil {
		t.Fatal(err)
	}
	lock, err := filelock.Acquire(context.Background(), filepath.Join(root, ".wago-release.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	command := exec.Command(child, "-test.run=^TestScheduleTargetRemovalDeletesContainingDirectoryAfterExit$")
	command.Env = append(os.Environ(), "WAGO_TEST_DEFERRED_CLEANUP_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run deferred cleanup child: %v\n%s", err, output)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(child); err != nil {
		t.Fatalf("worker bypassed publication lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	entries, err := os.ReadDir(root)
	for _, entry := range entries {
		t.Logf("remaining entry: %s", entry.Name())
	}
	t.Fatalf("deferred cleanup kept %s (read directory: %v)", root, err)
}

func TestScheduleTargetRemovalFindsRunningPayload(t *testing.T) {
	if root := os.Getenv("WAGO_TEST_RELEASE_CLEANUP_CHILD"); root != "" {
		launcher := filepath.Join(root, "wago.exe")
		deferred, err := ScheduleTargetRemoval(launcher, []string{filepath.Join(root, ".wago-releases"), launcher}, filepath.Join(root, ".wago-release.lock"), nil)
		if err != nil || !deferred {
			t.Fatalf("schedule payload cleanup: deferred=%v, err=%v", deferred, err)
		}
		return
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".wago-release.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	releases := filepath.Join(root, ".wago-releases")
	child := filepath.Join(releases, "release-test", "wago.exe")
	if err := os.MkdirAll(filepath.Dir(child), 0755); err != nil {
		t.Fatal(err)
	}
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, data, 0755); err != nil {
		t.Fatal(err)
	}
	launcher, sibling := filepath.Join(root, "wago.exe"), filepath.Join(root, "other-tool")
	for _, path := range []string{launcher, sibling} {
		if err := os.WriteFile(path, []byte("keep until cleanup"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(child, "-test.run=^TestScheduleTargetRemovalFindsRunningPayload$")
	command.Env = append(os.Environ(), "WAGO_TEST_RELEASE_CLEANUP_CHILD="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run payload cleanup child: %v\n%s", err, output)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, releaseErr := os.Stat(releases)
		_, launcherErr := os.Stat(launcher)
		_, pendingErr := os.Stat(managedrelease.UninstallPendingPath(filepath.Join(root, ".wago-release.lock")))
		if os.IsNotExist(releaseErr) && os.IsNotExist(launcherErr) && os.IsNotExist(pendingErr) {
			if _, err := os.Stat(sibling); err != nil {
				t.Fatalf("cleanup removed unrelated sibling: %v", err)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("deferred cleanup kept managed release artifacts")
}

func TestDeferredCleanupRejectsRetiredCoordinator(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, ".wago-release.lock")
	old, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	var identity syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(old.Descriptor()), &identity); err != nil {
		t.Fatal(err)
	}
	// Keep a renamed old inode to prevent file-ID recycling in this fixture.
	if err := os.Rename(lockPath, lockPath+".retired"); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "new-manager.exe")
	if err := os.WriteFile(target, []byte("keep new installation"), 0755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "cleanup.ps1")
	if err := os.WriteFile(script, []byte(targetRemovalScript(0, []string{target}, lockPath, nil, identity)), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script)
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "Wago publication lock was retired") {
		t.Fatalf("retired worker result: %v\n%s", err, output)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "keep new installation" {
		t.Fatalf("new installation changed: %q, %v", data, err)
	}
}

func TestFailedCleanupWorkerStartClearsPendingIntent(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(t.TempDir(), ".wago-release.lock")
	owner, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	t.Setenv("PATH", t.TempDir())
	deferred, err := ScheduleTargetRemoval(executable, []string{executable}, lockPath, nil)
	if err == nil || deferred {
		t.Fatalf("worker unexpectedly started without PowerShell: deferred=%v, err=%v", deferred, err)
	}
	if _, err := os.Stat(managedrelease.UninstallPendingPath(lockPath)); !os.IsNotExist(err) {
		t.Fatalf("failed start left pending cleanup: %v", err)
	}
}

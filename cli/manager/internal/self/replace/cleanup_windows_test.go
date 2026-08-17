//go:build windows

package replace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTargetRemovalScriptWaitsThenRemovesTargets(t *testing.T) {
	script := targetRemovalScript([]string{`C:\Users\A%lice\.wago`, `C:\Users\A%lice\.local\bin\wago.exe`})
	for _, want := range []string{
		`timeout /t 2 /nobreak`,
		`rmdir /s /q "C:\Users\A%%lice\.wago"`,
		`del /f /q "C:\Users\A%%lice\.local\bin\wago.exe"`,
		`del "%~f0"`,
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
		deferred, err := ScheduleTargetRemoval(executable, []string{filepath.Dir(executable)})
		if err != nil {
			t.Fatal(err)
		}
		if !deferred {
			t.Fatal("ScheduleTargetRemoval did not defer cleanup")
		}
		return
	}

	root := t.TempDir()
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
	command := exec.Command(child, "-test.run=^TestScheduleTargetRemovalDeletesContainingDirectoryAfterExit$")
	command.Env = append(os.Environ(), "WAGO_TEST_DEFERRED_CLEANUP_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run deferred cleanup child: %v\n%s", err, output)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("deferred cleanup kept %s", root)
}

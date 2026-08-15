//go:build linux

package watchsupervisor

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEnvironmentReplacesInternalValues(t *testing.T) {
	input := []string{
		markerEnvironment + "=old",
		"WAGO_TEST=value",
		guestExecutableEnvironment + "=/old",
	}
	want := "WAGO_TEST=value\n" + markerEnvironment + "=1\n" + guestExecutableEnvironment + "=/guest"
	if got := strings.Join(Environment(input, "/guest"), "\n"); got != want {
		t.Fatalf("supervisor environment = %q, want %q", got, want)
	}
}

func TestRunReapsAdoptedChildrenWhileGuestRuns(t *testing.T) {
	const testName = "^TestRunReapsAdoptedChildrenWhileGuestRuns$"
	if os.Getenv("WAGO_WATCH_REAPER_GUEST_TEST") == "1" {
		command := exec.Command("/bin/sh", "-c", "sleep 0.05 >/dev/null 2>&1 & echo $!")
		output, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		pids := strconv.Itoa(os.Getpid()) + " " + strings.TrimSpace(string(output))
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_REAPER_PID"), []byte(pids), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second)
		return
	}

	pidPath := t.TempDir() + "/child.pid"
	guest := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
	guest.Env = append(os.Environ(),
		"WAGO_WATCH_REAPER_GUEST_TEST=1",
		"WAGO_WATCH_REAPER_PID="+pidPath,
	)
	done := make(chan error, 1)
	go func() {
		_, err := Run(guest)
		done <- err
	}()
	var guestPID int
	t.Cleanup(func() {
		if guestPID != 0 {
			_ = syscall.Kill(guestPID, syscall.SIGKILL)
		}
		<-done
	})
	deadline := time.Now().Add(5 * time.Second)
	var childPID int
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidPath); err == nil {
			fields := strings.Fields(string(data))
			if len(fields) == 2 {
				guestPID, _ = strconv.Atoi(fields[0])
				childPID, _ = strconv.Atoi(fields[1])
			}
			if guestPID != 0 && childPID != 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("guest did not report its adopted child")
	}
	for time.Now().Before(deadline) {
		if _, err := os.Stat("/proc/" + strconv.Itoa(childPID)); errors.Is(err, os.ErrNotExist) {
			if err := syscall.Kill(guestPID, 0); err != nil {
				t.Fatalf("guest exited before adopted child was reaped: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("adopted child %d was not reaped while guest ran", childPID)
}

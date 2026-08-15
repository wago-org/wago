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

func TestMain(main *testing.M) {
	Enter()
	os.Exit(main.Run())
}

func TestProbeAcceptsCurrentSupervisor(t *testing.T) {
	if err := Probe(os.Args[0]); err != nil {
		t.Fatalf("probe current supervisor: %v", err)
	}
}

func TestProbeRejectsExecutableWithoutProtocol(t *testing.T) {
	executable, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}
	if err := Probe(executable); err == nil {
		t.Fatal("probe accepted executable without supervisor protocol")
	}
}

func TestEnvironmentReplacesInternalValues(t *testing.T) {
	input := []string{
		markerEnvironment + "=old",
		"WAGO_TEST=value",
		guestExecutableEnvironment + "=/old",
	}
	want := "WAGO_TEST=value\n" + markerEnvironment + "=" + guardianRole + "\n" + guestExecutableEnvironment + "=/guest"
	if got := strings.Join(Environment(input, "/guest"), "\n"); got != want {
		t.Fatalf("supervisor environment = %q, want %q", got, want)
	}
}

func TestGuardianDeathStopsWorkerTree(t *testing.T) {
	Enter()
	const testName = "^TestGuardianDeathStopsWorkerTree$"
	if os.Getenv("WAGO_WATCH_PARENT_DEATH_GUEST_TEST") == "1" {
		child := exec.Command("/bin/sh", "-c", "sleep 10")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		pids := strconv.Itoa(os.Getppid()) + " " + strconv.Itoa(os.Getpid()) + " " + strconv.Itoa(child.Process.Pid)
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_PARENT_DEATH_PIDS"), []byte(pids), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second)
		return
	}

	pidPath := t.TempDir() + "/tree.pids"
	guardian := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
	guardian.Env = Environment(append(os.Environ(),
		"WAGO_WATCH_PARENT_DEATH_GUEST_TEST=1",
		"WAGO_WATCH_PARENT_DEATH_PIDS="+pidPath,
	), os.Args[0])
	if err := guardian.Start(); err != nil {
		t.Fatal(err)
	}
	var pids []int
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		_ = guardian.Process.Kill()
		_ = guardian.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidPath); err == nil {
			candidate := make([]int, 0, 3)
			for _, field := range strings.Fields(string(data)) {
				if pid, parseErr := strconv.Atoi(field); parseErr == nil {
					candidate = append(candidate, pid)
				}
			}
			if len(candidate) == 3 {
				pids = candidate
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pids) != 3 {
		t.Fatal("guarded guest did not report its process tree")
	}
	if err := guardian.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = guardian.Wait()
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range pids {
			if processState(pid) != "" && processState(pid) != "Z" {
				alive = true
			}
		}
		if !alive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("guarded processes survived guardian death: %d=%s %d=%s %d=%s", pids[0], processState(pids[0]), pids[1], processState(pids[1]), pids[2], processState(pids[2]))
}

func processState(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	close := strings.LastIndex(string(data), ") ")
	if close < 0 {
		return "?"
	}
	fields := strings.Fields(string(data[close+2:]))
	if len(fields) == 0 {
		return "?"
	}
	return fields[0]
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

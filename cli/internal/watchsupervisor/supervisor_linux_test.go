//go:build linux

package watchsupervisor

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
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

func TestSignalRelayForwardsGuestGroupInterrupt(t *testing.T) {
	group := exec.Command("sleep", "30")
	group.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := group.Start(); err != nil {
		t.Fatal(err)
	}
	groupDone := false
	t.Cleanup(func() {
		if groupDone {
			return
		}
		_ = group.Process.Kill()
		_ = group.Wait()
	})
	relay, err := StartSignalRelay(os.Args[0], []string{"-test.run=^TestSignalRelayForwardsGuestGroupInterrupt$", "-test.count=1"}, os.Environ(), group.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	if err := syscall.Kill(-group.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-interrupts:
	case <-time.After(5 * time.Second):
		t.Fatal("signal relay did not forward the guest group interrupt")
	}
	if err := group.Wait(); err == nil {
		t.Fatal("guest process group ignored SIGINT")
	}
	groupDone = true
}

func TestSignalProcessIdentityRejectsChangedStart(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	t.Cleanup(func() {
		if finished {
			return
		}
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	process, ok := processByPID(child.Process.Pid)
	if !ok {
		t.Fatal("inspect child process")
	}
	changed := process
	changed.started++
	if err := signalProcessIdentity(changed, syscall.SIGKILL); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("changed process identity signal = %v, want process done", err)
	}
	if err := child.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("changed identity killed child: %v", err)
	}
	if err := signalProcessIdentity(process, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("valid process identity did not kill child")
	}
	finished = true
}

func TestRunPropagatesChildStopAndContinue(t *testing.T) {
	const testName = "^TestRunPropagatesChildStopAndContinue$"
	switch os.Getenv("WAGO_WATCH_STOP_ROLE") {
	case "child":
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_STOP_READY"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_STOP_CONTINUED"), []byte("continued"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	case "supervisor":
		child := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
		child.Env = append(os.Environ(), "WAGO_WATCH_STOP_ROLE=child")
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		code, err := Run(child)
		if err != nil {
			t.Fatal(err)
		}
		if code != 0 {
			t.Fatalf("child exit code = %d, want 0", code)
		}
		return
	}

	directory := t.TempDir()
	readyPath := directory + "/ready"
	continuedPath := directory + "/continued"
	supervisor := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
	supervisor.Env = append(os.Environ(),
		"WAGO_WATCH_STOP_ROLE=supervisor",
		"WAGO_WATCH_STOP_READY="+readyPath,
		"WAGO_WATCH_STOP_CONTINUED="+continuedPath,
	)
	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	var childPID int
	finished := false
	t.Cleanup(func() {
		if finished {
			return
		}
		_ = supervisor.Process.Kill()
		_ = supervisor.Wait()
		if childPID > 0 {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(readyPath); err == nil {
			childPID, _ = strconv.Atoi(string(data))
			if childPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("child did not stop")
	}
	var status syscall.WaitStatus
	for time.Now().Before(deadline) {
		pid, err := syscall.Wait4(supervisor.Process.Pid, &status, syscall.WUNTRACED|syscall.WNOHANG, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pid == supervisor.Process.Pid && status.Stopped() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !status.Stopped() {
		t.Fatal("supervisor did not mirror child stop")
	}
	if err := supervisor.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(continuedPath); err == nil && string(data) == "continued" {
			if err := supervisor.Wait(); err != nil {
				t.Fatal(err)
			}
			finished = true
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("continued supervisor did not continue child")
}

func TestChildLifetimeSurvivesGoThreadExit(t *testing.T) {
	const testName = "^TestChildLifetimeSurvivesGoThreadExit$"
	if os.Getenv("WAGO_WATCH_LIFETIME_CHILD") == "1" {
		parent, exited, err := monitorParentLifetime()
		if err != nil {
			t.Fatal(err)
		}
		defer parent.Close()
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_LIFETIME_READY"), []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		select {
		case <-exited:
			return
		case <-time.After(5 * time.Second):
			t.Fatal("parent lifetime pipe did not close")
		}
	}

	readyPath := t.TempDir() + "/ready"
	child := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
	child.Env = append(os.Environ(),
		"WAGO_WATCH_LIFETIME_CHILD=1",
		"WAGO_WATCH_LIFETIME_READY="+readyPath,
	)
	lifetime, err := BindChild(child)
	if err != nil {
		t.Fatal(err)
	}
	defer lifetime.Close()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	lifetime.Started()
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(readyPath); err != nil {
		t.Fatal("lifetime child did not start")
	}
	threadExited := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		close(threadExited)
	}()
	<-threadExited
	runtime.GC()
	select {
	case err := <-done:
		t.Fatalf("thread exit closed process lifetime pipe: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not observe process lifetime close")
	}
}

func TestEnvironmentReplacesInternalValues(t *testing.T) {
	input := []string{
		markerEnvironment + "=old",
		"WAGO_TEST=value",
		guestExecutableEnvironment + "=/old",
		parentLifetimeFDEnvironment + "=9",
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

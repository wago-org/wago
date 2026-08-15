//go:build linux && !tinygo && !wago_lean

package run

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wago-org/wago/cli/internal/watchsupervisor"
	"golang.org/x/sys/unix"
)

func TestWatchedSupervisorCleansSanitizedDaemon(t *testing.T) {
	const testName = "^TestWatchedSupervisorCleansSanitizedDaemon$"
	if os.Getenv("WAGO_WATCH_SUPERVISOR_GUEST_TEST") == "1" {
		daemon := exec.Command("/bin/sh", "-c", "sleep 10")
		daemon.Env = []string{"PATH=/usr/bin:/bin"}
		daemon.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := daemon.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_SUPERVISOR_PID"), []byte(strconv.Itoa(daemon.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if os.Getenv("WAGO_WATCH_SUPERVISOR_ROOT_TEST") == "1" {
		guest := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
		guest.Env = append(os.Environ(), "WAGO_WATCH_SUPERVISOR_GUEST_TEST=1")
		code, err := watchsupervisor.Run(guest)
		if err != nil {
			t.Fatal(err)
		}
		if code != 0 {
			t.Fatalf("supervised guest exit code = %d, want 0", code)
		}
		return
	}

	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	supervisor := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
	supervisor.Env = append(os.Environ(),
		"WAGO_WATCH_SUPERVISOR_ROOT_TEST=1",
		"WAGO_WATCH_SUPERVISOR_PID="+pidPath,
	)
	if output, err := supervisor.CombinedOutput(); err != nil {
		t.Fatalf("supervisor: %v\n%s", err, output)
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("sanitized daemon %d survived supervisor cleanup: %v", pid, err)
	}
}

func TestWatchGuardianStopsWhenOuterWatcherDies(t *testing.T) {
	watchsupervisor.Enter()
	const testName = "^TestWatchGuardianStopsWhenOuterWatcherDies$"
	switch os.Getenv("WAGO_WATCH_PARENT_ROLE") {
	case "guest":
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_GUEST_PID"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(24 * time.Hour)
		return
	case "outer":
		environment := watchsupervisor.Environment(append(os.Environ(),
			"WAGO_WATCH_PARENT_ROLE=guest",
			"WAGO_WATCH_GUEST_PID="+os.Getenv("WAGO_WATCH_GUEST_PID"),
		), os.Args[0])
		child, err := startWatchedChild(watchOptions{
			executable:  os.Args[0],
			arguments:   []string{"-test.run=" + testName, "-test.count=1"},
			environment: environment,
			stdin:       nil,
			stdout:      io.Discard,
			stderr:      io.Discard,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_GUARDIAN_PID"), []byte(strconv.Itoa(child.command.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(24 * time.Hour)
		return
	}

	directory := t.TempDir()
	guardianPath := filepath.Join(directory, "guardian.pid")
	guestPath := filepath.Join(directory, "guest.pid")
	outer := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
	outer.Env = append(os.Environ(),
		"WAGO_WATCH_PARENT_ROLE=outer",
		"WAGO_WATCH_GUARDIAN_PID="+guardianPath,
		"WAGO_WATCH_GUEST_PID="+guestPath,
	)
	if err := outer.Start(); err != nil {
		t.Fatal(err)
	}
	pids := []int{outer.Process.Pid}
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		_ = outer.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	var guardianPID, workerPID, guestPID int
	var treeReady bool
	for time.Now().Before(deadline) {
		guardianData, guardianErr := os.ReadFile(guardianPath)
		guestData, guestErr := os.ReadFile(guestPath)
		if guardianErr == nil && guestErr == nil {
			guardianPID, guardianErr = strconv.Atoi(string(guardianData))
			guestPID, guestErr = strconv.Atoi(string(guestData))
			guest, guestOK := watchedProcess(guestPID)
			if guardianErr == nil && guestErr == nil && guestOK {
				workerPID = guest.parent
				worker, workerOK := watchedProcess(workerPID)
				guardian, guardianOK := watchedProcess(guardianPID)
				if workerOK && worker.parent == guardianPID && guardianOK && guardian.parent == outer.Process.Pid {
					treeReady = true
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !treeReady {
		t.Fatal("watch process tree did not start")
	}
	pids = append(pids, guardianPID, workerPID, guestPID)
	if err := outer.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = outer.Wait()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range pids[1:] {
			state := watchProcessState(pid)
			if state != "" && state != "Z" {
				alive = true
			}
		}
		if !alive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("watch tree survived outer death: guardian=%s worker=%s guest=%s", watchProcessState(guardianPID), watchProcessState(workerPID), watchProcessState(guestPID))
}

func watchProcessState(pid int) string {
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

func TestWatchTrackingExcludesLateDaemon(t *testing.T) {
	child, err := startWatchedChild(watchOptions{
		stopGrace:  250 * time.Millisecond,
		executable: "/bin/sh",
		arguments:  []string{"-c", "sleep 10"},
		stdin:      nil,
		stdout:     io.Discard,
		stderr:     io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _, _ = child.stop(250*time.Millisecond, nil) })
	pidPath := filepath.Join(t.TempDir(), "unrelated.pid")
	helper := exec.Command("/bin/sh", "-c", `sleep 10 >/dev/null 2>&1 & echo $! > "$1"`, "watch-helper", pidPath)
	if err := helper.Run(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	unrelatedPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(unrelatedPID, syscall.SIGKILL)
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(unrelatedPID, &status, 0, nil)
	})
	if err := child.platform.processes.refresh(); err != nil {
		t.Fatal(err)
	}
	child.platform.processes.mu.Lock()
	_, tracked := child.platform.processes.processes[unrelatedPID]
	child.platform.processes.mu.Unlock()
	if tracked {
		t.Fatal("existing watcher child was tracked as a guest descendant")
	}
}

func TestWatchFinishDoesNotReapUnrelatedChild(t *testing.T) {
	unrelated := exec.Command("/bin/true")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = unrelated.Process.Kill()
		_ = unrelated.Wait()
	})
	time.Sleep(50 * time.Millisecond)
	_, ok := watchedProcess(unrelated.Process.Pid)
	if !ok {
		t.Fatal("inspect unrelated child")
	}
	tracker := &watchedProcessTracker{
		owner: os.Getpid(), root: 1 << 30, processes: make(map[int]uint64),
	}
	finishWatchedProcessTracking(tracker)
	if err := unrelated.Wait(); err != nil {
		t.Fatalf("unrelated child wait: %v", err)
	}
}

func TestWatchReapsSeededWorkerAfterRootExit(t *testing.T) {
	if err := prepareWatchedProcessTracking(); err != nil {
		t.Fatal(err)
	}
	worker := exec.Command("/bin/true")
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	done := false
	t.Cleanup(func() {
		if done {
			return
		}
		_ = worker.Process.Kill()
		_ = worker.Wait()
	})
	identity, ok := watchedProcess(worker.Process.Pid)
	if !ok {
		t.Fatal("inspect seeded worker")
	}
	deadline := time.Now().Add(5 * time.Second)
	for identity.state != 'Z' && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		identity, ok = watchedProcess(worker.Process.Pid)
		if !ok {
			t.Fatal("seeded worker disappeared before reap")
		}
	}
	if identity.state != 'Z' {
		t.Fatal("seeded worker did not exit")
	}
	tracker := &watchedProcessTracker{
		owner: os.Getpid(), root: 1 << 30,
		processes: map[int]uint64{identity.pid: identity.started},
	}
	tracker.reapAdopted()
	if _, ok := watchedProcess(identity.pid); ok {
		t.Fatal("seeded worker remained after reap")
	}
	_ = worker.Process.Release()
	done = true
}

func newTestWatchProcessTracker(t *testing.T, rootPID int) *watchedProcessTracker {
	t.Helper()
	root, ok := watchedProcess(rootPID)
	if !ok {
		t.Fatal("inspect test root")
	}
	return &watchedProcessTracker{
		owner: os.Getpid(), root: rootPID, rootStart: root.started,
		processes: make(map[int]uint64),
	}
}

func TestWatchedTerminalRestoreAfterRelayClose(t *testing.T) {
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
	relay, err := watchsupervisor.StartSignalRelay(os.Args[0], []string{"-test.run=^TestWatchHelperProcess$", "-test.count=1"}, os.Environ(), group.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	if err := group.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := group.Wait(); err == nil {
		t.Fatal("process group leader ignored kill")
	}
	groupDone = true
	if watchedTerminalGroupCanRestore(nil, group.Process.Pid) {
		t.Fatal("live relay did not keep the process group active")
	}
	if err := relay.Close(); err != nil {
		t.Fatal(err)
	}
	relay = nil
	deadline := time.Now().Add(5 * time.Second)
	for !watchedTerminalGroupCanRestore(nil, group.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatal("closed relay kept the process group active")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWatchSupervisorContinuesAfterChildSIGINT(t *testing.T) {
	directory := t.TempDir()
	modulePath := directory + "/module.wasm"
	logPath := directory + "/starts.log"
	if err := os.WriteFile(modulePath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- superviseWatch(ctx, watchOptions{
			path:       modulePath,
			interval:   15 * time.Millisecond,
			debounce:   60 * time.Millisecond,
			stopGrace:  250 * time.Millisecond,
			executable: "/bin/sh",
			arguments:  []string{"-c", `printf 'start\n' >> "$1"; kill -INT $$`, "wago-watch-signal", logPath},
			stdin:      nil,
			stdout:     io.Discard,
			stderr:     io.Discard,
		})
	}()
	t.Cleanup(cancel)
	waitForActiveWatchLog(t, logPath, 1, done)
	if err := os.WriteFile(modulePath, []byte("final"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForActiveWatchLog(t, logPath, 2, done)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("supervisor exit = %v, want context cancellation", err)
	}
}

func TestWatchSignalSkipsTerminalGroupButReachesDetachedDescendant(t *testing.T) {
	const testName = "^TestWatchSignalSkipsTerminalGroupButReachesDetachedDescendant$"
	switch os.Getenv("WAGO_WATCH_SIGNAL_TREE_ROLE") {
	case "root":
		leaf := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
		leaf.Env = append(os.Environ(), "WAGO_WATCH_SIGNAL_TREE_ROLE=leaf")
		leaf.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := leaf.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_SIGNAL_TREE_PID"), []byte(strconv.Itoa(leaf.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(24 * time.Hour)
		return
	case "leaf":
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, syscall.SIGINT)
		defer signal.Stop(interrupts)
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_SIGNAL_TREE_READY"), []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		<-interrupts
		if err := os.WriteFile(os.Getenv("WAGO_WATCH_SIGNAL_TREE_LOG"), []byte("interrupt"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	directory := t.TempDir()
	pidPath := filepath.Join(directory, "leaf.pid")
	readyPath := filepath.Join(directory, "ready")
	logPath := filepath.Join(directory, "signal.log")
	root := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
	root.Env = append(os.Environ(),
		"WAGO_WATCH_SIGNAL_TREE_ROLE=root",
		"WAGO_WATCH_SIGNAL_TREE_PID="+pidPath,
		"WAGO_WATCH_SIGNAL_TREE_READY="+readyPath,
		"WAGO_WATCH_SIGNAL_TREE_LOG="+logPath,
	)
	if err := root.Start(); err != nil {
		t.Fatal(err)
	}
	var leafPID int
	t.Cleanup(func() {
		_ = root.Process.Kill()
		_ = root.Wait()
		if leafPID > 0 {
			_ = syscall.Kill(leafPID, syscall.SIGKILL)
			var status syscall.WaitStatus
			_, _ = syscall.Wait4(leafPID, &status, 0, nil)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, pidErr := os.ReadFile(pidPath)
		_, readyErr := os.Stat(readyPath)
		if pidErr == nil && readyErr == nil {
			leafPID, pidErr = strconv.Atoi(string(data))
			if pidErr == nil {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if leafPID == 0 {
		t.Fatal("detached descendant did not start")
	}
	tracker := newTestWatchProcessTracker(t, root.Process.Pid)
	platform := watchedChildPlatform{terminalFD: -1, processes: tracker}
	if err := signalWatchedProcessTreeExceptGroup(platform, root, syscall.SIGINT, syscall.Getpgrp()); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(logPath); err == nil && string(data) == "interrupt" {
			if err := syscall.Kill(root.Process.Pid, 0); err != nil {
				t.Fatalf("shared-group root received interrupt: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("detached descendant did not receive interrupt")
}

func TestWatchSignalSendsOnceToIsolatedGroup(t *testing.T) {
	const testName = "^TestWatchSignalSendsOnceToIsolatedGroup$"
	if os.Getenv("WAGO_WATCH_GROUP_LEAF") == "1" {
		recordWatchSignalCount(t, os.Getenv("WAGO_WATCH_GROUP_LEAF_READY"), os.Getenv("WAGO_WATCH_GROUP_LEAF_LOG"))
		return
	}
	if os.Getenv("WAGO_WATCH_GROUP_ROOT") == "1" {
		leaf := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
		leaf.Env = append(os.Environ(), "WAGO_WATCH_GROUP_LEAF=1")
		if err := leaf.Start(); err != nil {
			t.Fatal(err)
		}
		recordWatchSignalCount(t, os.Getenv("WAGO_WATCH_GROUP_ROOT_READY"), os.Getenv("WAGO_WATCH_GROUP_ROOT_LOG"))
		if err := leaf.Wait(); err != nil {
			t.Fatal(err)
		}
		return
	}

	directory := t.TempDir()
	rootReady := filepath.Join(directory, "root.ready")
	rootLog := filepath.Join(directory, "root.log")
	leafReady := filepath.Join(directory, "leaf.ready")
	leafLog := filepath.Join(directory, "leaf.log")
	root := exec.Command(os.Args[0], "-test.run="+testName, "-test.count=1")
	root.Env = append(os.Environ(),
		"WAGO_WATCH_GROUP_ROOT=1",
		"WAGO_WATCH_GROUP_ROOT_READY="+rootReady,
		"WAGO_WATCH_GROUP_ROOT_LOG="+rootLog,
		"WAGO_WATCH_GROUP_LEAF_READY="+leafReady,
		"WAGO_WATCH_GROUP_LEAF_LOG="+leafLog,
	)
	root.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := root.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-root.Process.Pid, syscall.SIGKILL)
		_ = root.Wait()
	})
	waitForWatchFiles(t, rootReady, leafReady)
	tracker := newTestWatchProcessTracker(t, root.Process.Pid)
	platform := watchedChildPlatform{terminalFD: -1, processes: tracker}
	if err := signalWatchedProcessTree(platform, root, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitForWatchFiles(t, rootLog, leafLog)
	for name, path := range map[string]string{"root": rootLog, "leaf": leafLog} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "1" {
			t.Fatalf("%s signal count = %q, want 1", name, data)
		}
	}
}

func recordWatchSignalCount(t *testing.T, readyPath, logPath string) {
	t.Helper()
	interrupts := make(chan os.Signal, 8)
	signal.Notify(interrupts, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	<-interrupts
	count := 1
	deadline := time.NewTimer(100 * time.Millisecond)
	defer deadline.Stop()
	for {
		select {
		case <-interrupts:
			count++
		case <-deadline.C:
			if err := os.WriteFile(logPath, []byte(strconv.Itoa(count)), 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
}

func waitForWatchFiles(t *testing.T, paths ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ready := true
		for _, path := range paths {
			if _, err := os.Stat(path); err != nil {
				ready = false
				break
			}
		}
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for files: %v", paths)
}

func TestWatchedProcessDescendantsScansEveryThread(t *testing.T) {
	if os.Getenv("WAGO_WATCH_THREAD_CHILD") == "1" {
		time.Sleep(24 * time.Hour)
	}
	type startedChild struct {
		command *exec.Cmd
		tid     int
		err     error
	}
	const workers = 8
	started := make(chan startedChild, workers)
	release := make(chan struct{})
	defer close(release)
	for range workers {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			tid := unix.Gettid()
			var command *exec.Cmd
			var err error
			if tid != os.Getpid() {
				command = exec.Command(os.Args[0], "-test.run=^TestWatchedProcessDescendantsScansEveryThread$", "-test.count=1")
				command.Env = append(os.Environ(), "WAGO_WATCH_THREAD_CHILD=1")
				err = command.Start()
			}
			started <- startedChild{command: command, tid: tid, err: err}
			<-release
		}()
	}
	children := make([]startedChild, 0, workers)
	var startErr error
	for range workers {
		child := <-started
		if child.err != nil && startErr == nil {
			startErr = child.err
		}
		if child.command != nil {
			children = append(children, child)
		}
	}
	t.Cleanup(func() {
		for _, child := range children {
			_ = child.command.Process.Kill()
			_ = child.command.Wait()
		}
	})
	if startErr != nil {
		t.Fatal(startErr)
	}
	if len(children) == 0 {
		t.Fatal("did not start a child from a non-leader thread")
	}
	descendants, err := watchedProcessDescendants(os.Getpid(), nil, maxWatchedDescendants)
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[int]bool, len(descendants))
	for _, process := range descendants {
		found[process.pid] = true
	}
	for _, child := range children {
		if !found[child.command.Process.Pid] {
			t.Fatalf("child %d from thread %d was not tracked", child.command.Process.Pid, child.tid)
		}
	}
}

func TestWatchedTerminalGroupCanRestoreEmptyGroup(t *testing.T) {
	group := 1 << 30
	for ; group > 1<<29; group-- {
		if errors.Is(syscall.Kill(-group, 0), syscall.ESRCH) {
			break
		}
	}
	if !watchedTerminalGroupCanRestore(nil, group) {
		t.Fatalf("empty foreground group %d was not restorable", group)
	}
	if watchedTerminalGroupCanRestore(nil, syscall.Getpgrp()) {
		t.Fatal("unowned live foreground group was restorable")
	}
}

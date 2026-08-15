//go:build linux && !tinygo && !wago_lean

package run

import (
	"bufio"
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

	"golang.org/x/sys/unix"
)

func TestWatchReapsAdoptedChildrenWhileRootRuns(t *testing.T) {
	if err := prepareWatchedProcessTracking(); err != nil {
		t.Fatal(err)
	}
	root, pid := startTestWatchRoot(t, "0.2")
	tracker := newTestWatchProcessTracker(t, root.Process.Pid)
	if err := startWatchedProcessTracking(tracker); err != nil {
		t.Fatal(err)
	}
	defer tracker.close()
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("adopted child %d was not reaped", pid)
}

func TestWatchReapsChildPresentAtTrackingStart(t *testing.T) {
	if err := prepareWatchedProcessTracking(); err != nil {
		t.Fatal(err)
	}
	root, pid := startTestWatchRoot(t, "0.05")
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	time.Sleep(100 * time.Millisecond)
	tracker := newTestWatchProcessTracker(t, root.Process.Pid)
	if err := startWatchedProcessTracking(tracker); err != nil {
		t.Fatal(err)
	}
	tracker.close()
	if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child present at tracking start was not reaped: %v", err)
	}
}

func startTestWatchRoot(t *testing.T, childDelay string) (*exec.Cmd, int) {
	t.Helper()
	command := exec.Command("/bin/sh", "-c", "/bin/sh -c 'sleep "+childDelay+" & echo $!'; sleep 10")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	return command, pid
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
	descendants, err := watchedProcessDescendants(os.Getpid(), os.Getpid(), nil, maxWatchedDescendants)
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

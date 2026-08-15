//go:build linux && !tinygo && !wago_lean

package run

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
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
	tracker := newTestWatchProcessTracker(t)
	if err := startWatchedProcessTracking(tracker); err != nil {
		t.Fatal(err)
	}
	defer tracker.close()
	output, err := exec.Command("/bin/sh", "-c", "sleep 0.2 & echo $!").Output()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatal(err)
	}
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
	output, err := exec.Command("/bin/sh", "-c", "sleep 0.05 & echo $!").Output()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	time.Sleep(100 * time.Millisecond)
	tracker := newTestWatchProcessTracker(t)
	if err := startWatchedProcessTracking(tracker); err != nil {
		t.Fatal(err)
	}
	tracker.close()
	if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child present at tracking start was not reaped: %v", err)
	}
}

func newTestWatchProcessTracker(t *testing.T) *watchedProcessTracker {
	t.Helper()
	owner, ok := watchedProcess(os.Getpid())
	if !ok {
		t.Fatal("inspect test process")
	}
	return &watchedProcessTracker{
		owner: os.Getpid(), root: os.Getpid(), rootStart: owner.started,
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

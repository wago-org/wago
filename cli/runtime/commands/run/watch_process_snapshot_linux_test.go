//go:build linux && !tinygo && !wago_lean

package run

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWatchedProcessDescendantsScansEveryThread(t *testing.T) {
	if os.Getenv("WAGO_WATCH_THREAD_CHILD") == "1" {
		select {}
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

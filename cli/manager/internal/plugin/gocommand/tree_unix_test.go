//go:build linux || darwin

package gocommand

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCompletedGoLeaderStopsPipeHoldingDescendant(t *testing.T) {
	dir := t.TempDir()
	childReady := filepath.Join(dir, "child-ready")
	release := filepath.Join(dir, "release")
	survived := filepath.Join(dir, "survived")
	// The direct shell exits successfully while its same-group child keeps
	// stdout open. The group must be signaled on success, not just cancel.
	script := "(: > \"$WAGO_CHILD_READY\"; " +
		"while [ ! -e \"$WAGO_RELEASE\" ]; do sleep 0.01; done; " +
		": > \"$WAGO_SURVIVED\") 2>/dev/null & " +
		"while [ ! -e \"$WAGO_CHILD_READY\" ]; do sleep 0.01; done; exit 0"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := &Command{Cmd: exec.CommandContext(ctx, "sh", "-c", script), ctx: ctx}
	command.WaitDelay = time.Second
	command.Env = append(os.Environ(), "WAGO_CHILD_READY="+childReady, "WAGO_RELEASE="+release, "WAGO_SURVIVED="+survived)
	defer os.WriteFile(release, nil, 0o600)
	done := make(chan error, 1)
	go func() {
		_, err := command.CombinedOutput()
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(childReady); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("completed command returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("completed command remained blocked on descendant pipe")
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(survived); err == nil {
		t.Fatal("descendant survived normal direct-process exit")
	}
}

func TestCompletedGoLeaderStopsStdoutPipeHolder(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	survived := filepath.Join(dir, "survived")
	script := "(while [ ! -e \"$WAGO_RELEASE\" ]; do sleep 0.01; done; " +
		": > \"$WAGO_SURVIVED\") & printf 'done\\n'; exit 0"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := &Command{Cmd: exec.CommandContext(ctx, "sh", "-c", script), ctx: ctx}
	command.Env = append(os.Environ(), "WAGO_RELEASE="+release, "WAGO_SURVIVED="+survived)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "done\n" {
		t.Fatalf("stdout = %q", output)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(survived); err == nil {
		t.Fatal("stdout holder survived normal direct-process exit")
	}
}

func TestDetachedPipeHolderIsBoundedBeforeLeaderReap(t *testing.T) {
	if os.Getenv("WAGO_DETACHED_PIPE_HELPER") == "1" {
		if _, err := syscall.Setsid(); err != nil {
			os.Exit(3)
		}
		_ = os.WriteFile(os.Getenv("WAGO_CHILD_READY"), nil, 0o600)
		for {
			if _, err := os.Stat(os.Getenv("WAGO_RELEASE")); err == nil {
				_ = os.WriteFile(os.Getenv("WAGO_HELPER_DONE"), nil, 0o600)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	helperDone := filepath.Join(dir, "helper-done")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	defer os.WriteFile(release, nil, 0o600)
	// The helper leaves the shell's process group yet holds its output pipe.
	// This exercises the bounded drain without permitting any late signal
	// against the shell's numeric PGID after it is reaped.
	command := &Command{Cmd: exec.CommandContext(ctx, "sh", "-c", "\"$WAGO_TEST_BINARY\" -test.run '^TestDetachedPipeHolderIsBoundedBeforeLeaderReap$' & while [ ! -e \"$WAGO_CHILD_READY\" ]; do sleep 0.01; done; exit 0"), ctx: ctx}
	command.Env = append(os.Environ(), "WAGO_DETACHED_PIPE_HELPER=1", "WAGO_TEST_BINARY="+os.Args[0], "WAGO_CHILD_READY="+ready, "WAGO_RELEASE="+release, "WAGO_HELPER_DONE="+helperDone)
	start := time.Now()
	_, err := command.CombinedOutput()
	if err != exec.ErrWaitDelay {
		t.Fatalf("detached pipe holder returned %v, want WaitDelay", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second || elapsed > 3*time.Second {
		t.Fatalf("detached pipe drain took %v, want bounded one-second grace", elapsed)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); ; {
		if _, err := os.Stat(helperDone); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detached test helper did not exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOutputPreservesExitErrorAndStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := &Command{Cmd: exec.CommandContext(ctx, "sh", "-c", "printf 'bad stderr' >&2; exit 7"), ctx: ctx}
	_, err := command.Output()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 7 {
		t.Fatalf("Output error = %v, want exit code 7", err)
	}
	if string(exit.Stderr) != "bad stderr" {
		t.Fatalf("captured stderr = %q", exit.Stderr)
	}
}

func TestBeforeWaitReportsGroupSignalFailure(t *testing.T) {
	tree := &commandTree{
		exitDone: make(chan struct{}),
		copyDone: make(chan struct{}),
		killErr:  syscall.EPERM,
	}
	close(tree.exitDone)
	close(tree.copyDone)
	if err := tree.beforeWait(); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("beforeWait error = %v, want group signal failure", err)
	}
}

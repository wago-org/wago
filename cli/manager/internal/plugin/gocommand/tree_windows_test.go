//go:build windows

package gocommand

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCanceledCommandTerminatesWindowsJobDescendants(t *testing.T) {
	switch os.Getenv("WAGO_TEST_COMMAND_TREE_MODE") {
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=TestCanceledCommandTerminatesWindowsJobDescendants")
		child.Env = append(os.Environ(), "WAGO_TEST_COMMAND_TREE_MODE=child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("WAGO_TEST_COMMAND_TREE_READY"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_ = child.Wait()
		return
	case "child":
		for {
			if _, err := os.Stat(os.Getenv("WAGO_TEST_COMMAND_TREE_RELEASE")); err == nil {
				if err := os.WriteFile(os.Getenv("WAGO_TEST_COMMAND_TREE_SURVIVED"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	survived := filepath.Join(dir, "survived")
	t.Setenv("WAGO_TEST_COMMAND_TREE_MODE", "parent")
	t.Setenv("WAGO_TEST_COMMAND_TREE_READY", ready)
	t.Setenv("WAGO_TEST_COMMAND_TREE_RELEASE", release)
	t.Setenv("WAGO_TEST_COMMAND_TREE_SURVIVED", survived)
	defer os.WriteFile(release, nil, 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := &Command{Cmd: exec.CommandContext(ctx, os.Args[0], "-test.run=TestCanceledCommandTerminatesWindowsJobDescendants"), ctx: ctx}
	command.WaitDelay = time.Second
	done := make(chan error, 1)
	go func() {
		_, err := command.CombinedOutput()
		done <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job parent did not start its child")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled Windows job returned success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled Windows job did not stop")
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(survived); err == nil {
		t.Fatal("compiler-style descendant survived job cancellation")
	}
}

func TestWindowsJobAllowsSuccessfulCommand(t *testing.T) {
	if os.Getenv("WAGO_TEST_COMMAND_TREE_MODE") == "success" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := &Command{Cmd: exec.CommandContext(ctx, os.Args[0], "-test.run=TestWindowsJobAllowsSuccessfulCommand"), ctx: ctx}
	command.Env = append(os.Environ(), "WAGO_TEST_COMMAND_TREE_MODE=success")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("successful Go-style command failed in a Windows job: %v: %s", err, output)
	}
}

func TestWindowsCompletedLeaderStopsStdoutPipeHolder(t *testing.T) {
	switch os.Getenv("WAGO_TEST_COMMAND_TREE_MODE") {
	case "pipe_parent":
		child := exec.Command(os.Args[0], "-test.run=TestWindowsCompletedLeaderStopsStdoutPipeHolder")
		child.Env = append(os.Environ(), "WAGO_TEST_COMMAND_TREE_MODE=pipe_child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("WAGO_TEST_COMMAND_TREE_READY"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	case "pipe_child":
		for {
			if _, err := os.Stat(os.Getenv("WAGO_TEST_COMMAND_TREE_RELEASE")); err == nil {
				_ = os.WriteFile(os.Getenv("WAGO_TEST_COMMAND_TREE_SURVIVED"), nil, 0o600)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	survived := filepath.Join(dir, "survived")
	t.Setenv("WAGO_TEST_COMMAND_TREE_MODE", "pipe_parent")
	t.Setenv("WAGO_TEST_COMMAND_TREE_READY", ready)
	t.Setenv("WAGO_TEST_COMMAND_TREE_RELEASE", release)
	t.Setenv("WAGO_TEST_COMMAND_TREE_SURVIVED", survived)
	defer os.WriteFile(release, nil, 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := &Command{Cmd: exec.CommandContext(ctx, os.Args[0], "-test.run=TestWindowsCompletedLeaderStopsStdoutPipeHolder"), ctx: ctx}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	stopClose := context.AfterFunc(ctx, func() { _ = stdout.Close() })
	_, readErr := io.ReadAll(stdout)
	stopClose()
	waitErr := command.Wait()
	if readErr != nil || waitErr != nil {
		t.Fatalf("stdout remained held after Go leader exit: read=%v, wait=%v", readErr, waitErr)
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatalf("job child was not started before leader exit: %v", err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(survived); err == nil {
		t.Fatal("job child survived successful Go leader exit")
	}
}

func TestWindowsCancelBeforeAttachStillSignalsAssignedJob(t *testing.T) {
	if os.Getenv("WAGO_TEST_COMMAND_TREE_MODE") == "early_cancel_child" {
		time.Sleep(10 * time.Second)
		return
	}
	command := exec.CommandContext(context.Background(), os.Args[0], "-test.run=TestWindowsCancelBeforeAttachStillSignalsAssignedJob")
	command.Env = append(os.Environ(), "WAGO_TEST_COMMAND_TREE_MODE=early_cancel_child")
	tree, err := prepareCommandTree(command)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.close()
	// Simulate CommandContext's callback firing after Start but before
	// assignment. It must not mark the empty job as permanently signaled.
	if err := tree.signalJob(); err != nil || tree.signaled {
		t.Fatalf("empty-job cancellation = %v, signaled=%v", err, tree.signaled)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	if err := tree.attach(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if err := tree.terminateAndWait(); err != nil {
		t.Fatalf("assigned job ignored early cancellation: %v", err)
	}
	if err := tree.beforeWait(); err != nil {
		t.Fatal(err)
	}
}

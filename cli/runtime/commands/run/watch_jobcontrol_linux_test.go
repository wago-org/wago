//go:build linux && !tinygo

package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestWatchSupervisorMirrorsTerminalJobControl(t *testing.T) {
	switch os.Getenv("WAGO_WATCH_JOB_CONTROL_HELPER") {
	case "wrapper":
		runWatchJobControlWrapper(t)
		return
	case "watcher":
		runWatchJobControlHelper(t)
		return
	}
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "module.wasm")
	logPath := filepath.Join(dir, "starts.log")
	if err := os.WriteFile(modulePath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	master, slave := openWatchPTY(t)
	defer master.Close()
	termios, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	termios.Lflag |= unix.TOSTOP
	if err := unix.IoctlSetTermios(int(slave.Fd()), unix.TCSETS, termios); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestWatchSupervisorMirrorsTerminalJobControl$", "-test.count=1")
	command.Env = append(os.Environ(),
		"WAGO_WATCH_JOB_CONTROL_HELPER=wrapper",
		"WAGO_WATCH_MODULE="+modulePath,
		"WAGO_WATCH_LOG="+logPath,
	)
	command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := command.Start(); err != nil {
		slave.Close()
		t.Fatal(err)
	}
	if err := slave.Close(); err != nil {
		t.Fatal(err)
	}
	finished := false
	t.Cleanup(func() {
		if finished {
			return
		}
		foreground, _ := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPGRP)
		if foreground > 0 {
			_ = syscall.Kill(-foreground, syscall.SIGKILL)
		}
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_, _ = command.Process.Wait()
	})
	output := watchPTYOutput(master)
	waitForWatchPTYOutput(t, output, "watching")
	waitForWatchLog(t, logPath, 1)
	if err := os.Remove(modulePath); err != nil {
		t.Fatal(err)
	}
	waitForWatchPTYOutput(t, output, "watch:")
	waitForWatchForegroundGroup(t, master, command.Process.Pid)
	foreground, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPGRP)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-foreground, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	waitForWatchedProcessStop(t, command.Process.Pid)
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	waitForWatchForegroundGroup(t, master, command.Process.Pid)
	if _, err := master.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("job-control helper: %v", err)
	}
	finished = true
}

func runWatchJobControlWrapper(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestWatchSupervisorMirrorsTerminalJobControl$", "-test.count=1")
	command.Env = append(os.Environ(), "WAGO_WATCH_JOB_CONTROL_HELPER=watcher")
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		t.Fatalf("watcher helper: %v", err)
	}
}

func runWatchJobControlHelper(t *testing.T) {
	signal.Reset(syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	options := watchTestOptions(os.Getenv("WAGO_WATCH_MODULE"), os.Getenv("WAGO_WATCH_LOG"), "", false)
	options.stdin, options.stdout, options.stderr = os.Stdin, os.Stdout, os.Stderr
	err := superviseWatch(context.Background(), options)
	var interrupted *watchInterruptedError
	if !errors.As(err, &interrupted) || interrupted.signal != os.Interrupt {
		t.Fatalf("watch supervisor exit = %v, want interrupt", err)
	}
}

func openWatchPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(masterFD), "watch-pty-master")
	if err := unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	return master, slave
}

func watchPTYOutput(master *os.File) <-chan string {
	output := make(chan string, 16)
	go func() {
		defer close(output)
		buffer := make([]byte, 4096)
		for {
			count, err := master.Read(buffer)
			if count != 0 {
				output <- string(buffer[:count])
			}
			if err != nil {
				return
			}
		}
	}()
	return output
}

func waitForWatchPTYOutput(t *testing.T, output <-chan string, match string) {
	t.Helper()
	var received strings.Builder
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case value, ok := <-output:
			if !ok {
				t.Fatalf("PTY closed before %q; output: %q", match, received.String())
			}
			received.WriteString(value)
			if strings.Contains(received.String(), match) {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %q; output: %q", match, received.String())
		}
	}
}

func waitForWatchedProcessStop(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var status syscall.WaitStatus
		found, err := syscall.Wait4(pid, &status, syscall.WUNTRACED|syscall.WNOHANG, nil)
		if err != nil {
			t.Fatal(err)
		}
		if found == pid && status.Stopped() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watcher did not mirror the child stop")
}

func waitForWatchForegroundGroup(t *testing.T, master *os.File, watcher int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	lastForeground := -1
	for time.Now().Before(deadline) {
		foreground, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPGRP)
		if err != nil {
			t.Fatal(err)
		}
		if foreground != watcher {
			return
		}
		lastForeground = foreground
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("watched child did not regain the foreground terminal: foreground=%d watcher=%d", lastForeground, watcher)
}

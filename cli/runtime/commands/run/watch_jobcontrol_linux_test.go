//go:build linux && !tinygo && !wago_lean

package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestWatchSupervisorMirrorsTerminalJobControl(t *testing.T) {
	switch os.Getenv("WAGO_WATCH_JOB_CONTROL_HELPER") {
	case "watcher":
		runWatchJobControlHelper(t)
		return
	}
	for _, test := range []struct {
		name               string
		separateForeground bool
	}{
		{name: "shared foreground"},
		{name: "separate guest foreground", separateForeground: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			testWatchSupervisorMirrorsTerminalJobControl(t, test.separateForeground)
		})
	}
}

func testWatchSupervisorMirrorsTerminalJobControl(t *testing.T, separateForeground bool) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "module.wasm")
	logPath := filepath.Join(dir, "starts.log")
	foregroundPath := filepath.Join(dir, "foreground")
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
		"WAGO_WATCH_JOB_CONTROL_HELPER=watcher",
		"WAGO_WATCH_MODULE="+modulePath,
		"WAGO_WATCH_LOG="+logPath,
	)
	if separateForeground {
		command.Env = append(command.Env,
			"WAGO_WATCH_SEPARATE_FOREGROUND=1",
			"WAGO_WATCH_FOREGROUND_GROUP="+foregroundPath,
		)
	}
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
	wantForeground := command.Process.Pid
	if separateForeground {
		wantForeground = waitForWatchForegroundFile(t, foregroundPath)
	}
	if err := os.Remove(modulePath); err != nil {
		t.Fatal(err)
	}
	waitForWatchPTYOutput(t, output, "watch:")
	waitForWatchForegroundGroup(t, master, wantForeground)
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
	waitForWatchForegroundGroup(t, master, wantForeground)
	if separateForeground {
		if err := syscall.Kill(-wantForeground, syscall.SIGCONT); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(-wantForeground, syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := master.Write([]byte{0x03}); err != nil {
			t.Fatal(err)
		}
	}
	commandDone := make(chan error, 1)
	go func() { commandDone <- command.Wait() }()
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatalf("job-control helper: %v", err)
		}
	case <-time.After(5 * time.Second):
		foreground, _ := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPGRP)
		log, _ := os.ReadFile(logPath)
		if foreground > 0 {
			_ = syscall.Kill(-foreground, syscall.SIGKILL)
		}
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-commandDone
		finished = true
		t.Fatalf("job-control helper did not stop after Ctrl-C: foreground=%d log=%q", foreground, log)
	}
	finished = true
	if got, want := waitForWatchLog(t, logPath, 2), []string{"first", "interrupt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("helper log = %#v, want %#v", got, want)
	}
}

func waitForWatchForegroundFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			group, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			return group
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("guest did not publish its foreground process group")
	return 0
}

func runWatchJobControlHelper(t *testing.T) {
	signal.Reset(syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	options := watchTestOptions(os.Getenv("WAGO_WATCH_MODULE"), os.Getenv("WAGO_WATCH_LOG"), "", false)
	options.environment = append(options.environment, "WAGO_WATCH_SIGNAL=1", "WAGO_WATCH_COUNT_SIGNALS=1")
	options.stdin, options.stdout, options.stderr = strings.NewReader(""), os.Stdout, os.Stderr
	signals := watchedSignals()
	interrupts := make(chan os.Signal, len(signals))
	signal.Notify(interrupts, signals...)
	defer signal.Stop(interrupts)
	options.interrupts = interrupts
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

func waitForWatchForegroundGroup(t *testing.T, master *os.File, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	lastForeground := -1
	for time.Now().Before(deadline) {
		foreground, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPGRP)
		if err != nil {
			t.Fatal(err)
		}
		if foreground == want {
			return
		}
		lastForeground = foreground
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("watch job did not regain the foreground terminal: foreground=%d want=%d", lastForeground, want)
}

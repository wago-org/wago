//go:build linux

// Package watchsupervisor runs a watched runtime below a provider-free Linux
// subreaper so every fork remains attributable to that runtime.
package watchsupervisor

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const markerEnvironment = "WAGO_WATCH_SUPERVISOR"
const guestExecutableEnvironment = "WAGO_WATCH_GUEST_EXECUTABLE"
const maxDescendants = 4096
const maxThreads = 4096
const stopGrace = time.Second
const cleanupGrace = 500 * time.Millisecond

type processInfo struct {
	pid, parent int
	started     uint64
}

// Environment marks a provider-free executable as a supervisor for guest.
func Environment(base []string, guest string) []string {
	if base == nil {
		base = os.Environ()
	}
	result := withoutInternalEnvironment(base)
	return append(result, markerEnvironment+"=1", guestExecutableEnvironment+"="+guest)
}

// Enter starts the marked guest and exits. It returns for normal invocations.
func Enter() {
	if os.Getenv(markerEnvironment) != "1" {
		return
	}
	guest := os.Getenv(guestExecutableEnvironment)
	if guest == "" {
		_, _ = fmt.Fprintln(os.Stderr, "wago: watch supervisor: missing guest executable")
		os.Exit(1)
	}
	command := exec.Command(guest, os.Args[1:]...)
	command.Env = withoutInternalEnvironment(os.Environ())
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	code, err := Run(command)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "wago: watch supervisor: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

func withoutInternalEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if strings.HasPrefix(entry, markerEnvironment+"=") || strings.HasPrefix(entry, guestExecutableEnvironment+"=") {
			continue
		}
		result = append(result, entry)
	}
	return result
}

// Run supervises command and returns its process exit code.
func Run(command *exec.Cmd) (int, error) {
	if err := prepare(); err != nil {
		return 1, err
	}
	events := make(chan os.Signal, 8)
	signal.Notify(events, os.Interrupt, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGCONT, syscall.SIGCHLD)
	defer signal.Stop(events)
	if err := command.Start(); err != nil {
		return 1, err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	for {
		select {
		case result := <-done:
			if cleanupErr := cleanup(); cleanupErr != nil {
				return 1, cleanupErr
			}
			return commandExitCode(result), nil
		case received := <-events:
			switch received {
			case syscall.SIGCHLD:
				if err := reapAdopted(command.Process.Pid); err != nil {
					_ = command.Process.Kill()
					<-done
					return 1, errors.Join(err, cleanup())
				}
				continue
			case syscall.SIGCONT:
				continue
			}
			timer := time.NewTimer(stopGrace)
			select {
			case <-done:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
				_ = command.Process.Kill()
				<-done
			}
			if cleanupErr := cleanup(); cleanupErr != nil {
				return 1, cleanupErr
			}
			return signalExitCode(received), nil
		}
	}
}

func prepare() error {
	children, err := os.Open("/proc/self/task/" + strconv.Itoa(os.Getpid()) + "/children")
	if err != nil {
		return fmt.Errorf("watch process tracking requires procfs child lists: %w", err)
	}
	if err := children.Close(); err != nil {
		return fmt.Errorf("watch process tracking requires procfs child lists: %w", err)
	}
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}

func reapAdopted(guestPID int) error {
	processes, scanErr := descendants(os.Getpid(), maxDescendants)
	for _, process := range processes {
		if process.parent != os.Getpid() || process.pid == guestPID {
			continue
		}
		current, ok := processByPID(process.pid)
		if !ok || current.started != process.started || current.parent != os.Getpid() {
			continue
		}
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(process.pid, &status, syscall.WNOHANG, nil)
	}
	return scanErr
}

func cleanup() error {
	deadline := time.Now().Add(cleanupGrace)
	var scanErr error
	for {
		processes, err := descendants(os.Getpid(), maxDescendants)
		if err != nil && scanErr == nil {
			scanErr = err
		}
		for _, process := range processes {
			current, ok := processByPID(process.pid)
			if !ok || current.started != process.started {
				continue
			}
			if killErr := syscall.Kill(process.pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
				scanErr = errors.Join(scanErr, killErr)
			}
		}
		reapAll()
		if len(processes) == 0 {
			return scanErr
		}
		if time.Now().After(deadline) {
			return errors.Join(scanErr, errors.New("watched process cleanup timed out"))
		}
		time.Sleep(time.Millisecond)
	}
}

func descendants(root, limit int) ([]processInfo, error) {
	rootProcess, ok := processByPID(root)
	if !ok {
		return nil, os.ErrProcessDone
	}
	queue := []processInfo{rootProcess}
	seen := map[int]bool{root: true}
	processes := make([]processInfo, 0)
	for len(queue) != 0 {
		parent := queue[0]
		queue = queue[1:]
		current, exists := processByPID(parent.pid)
		if !exists || current.started != parent.started {
			continue
		}
		tasks, err := processTasks(parent.pid, maxThreads)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return processes, err
		}
		for _, task := range tasks {
			file, openErr := os.Open("/proc/" + strconv.Itoa(parent.pid) + "/task/" + strconv.Itoa(task) + "/children")
			if errors.Is(openErr, os.ErrNotExist) {
				continue
			}
			if openErr != nil {
				return processes, openErr
			}
			scanner := bufio.NewScanner(file)
			scanner.Split(bufio.ScanWords)
			for scanner.Scan() {
				pid, parseErr := strconv.Atoi(scanner.Text())
				if parseErr != nil || seen[pid] {
					continue
				}
				process, childExists := processByPID(pid)
				if !childExists || process.parent != parent.pid {
					continue
				}
				if len(processes) >= limit {
					_ = file.Close()
					return processes, fmt.Errorf("watched process tree exceeds %d descendants", limit)
				}
				seen[pid] = true
				processes = append(processes, process)
				queue = append(queue, process)
			}
			scanErr := scanner.Err()
			closeErr := file.Close()
			if scanErr != nil {
				return processes, scanErr
			}
			if closeErr != nil {
				return processes, closeErr
			}
		}
	}
	return processes, nil
}

func processTasks(pid, limit int) ([]int, error) {
	directory, err := os.Open("/proc/" + strconv.Itoa(pid) + "/task")
	if err != nil {
		return nil, err
	}
	names, readErr := directory.Readdirnames(limit + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(names) > limit {
		return nil, fmt.Errorf("watched process exceeds %d threads", limit)
	}
	tasks := make([]int, 0, len(names))
	for _, name := range names {
		if task, parseErr := strconv.Atoi(name); parseErr == nil {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func processByPID(pid int) (processInfo, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return processInfo{}, false
	}
	close := strings.LastIndex(string(data), ") ")
	if close < 0 {
		return processInfo{}, false
	}
	fields := strings.Fields(string(data[close+2:]))
	if len(fields) <= 19 {
		return processInfo{}, false
	}
	parent, parentErr := strconv.Atoi(fields[1])
	started, startedErr := strconv.ParseUint(fields[19], 10, 64)
	if parentErr != nil || startedErr != nil {
		return processInfo{}, false
	}
	return processInfo{pid: pid, parent: parent, started: started}, true
}

func reapAll() {
	for {
		var status syscall.WaitStatus
		pid, _ := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if pid <= 0 {
			return
		}
	}
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return 1
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		return 1
	}
	if status.Signaled() {
		return 128 + int(status.Signal())
	}
	return status.ExitStatus()
}

func signalExitCode(value os.Signal) int {
	if signalValue, ok := value.(syscall.Signal); ok {
		return 128 + int(signalValue)
	}
	return 1
}

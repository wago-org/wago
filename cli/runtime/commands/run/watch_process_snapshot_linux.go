//go:build linux && !wago_lean

package run

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

func prepareWatchedProcessTracking() error {
	children, err := os.Open("/proc/self/task/" + strconv.Itoa(os.Getpid()) + "/children")
	if err != nil {
		return fmt.Errorf("watch process tracking requires procfs child lists: %w", err)
	}
	if err := children.Close(); err != nil {
		return fmt.Errorf("watch process tracking requires procfs child lists: %w", err)
	}
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}

func watchedProcessTrackingBaseline() (map[int]uint64, error) {
	owner := os.Getpid()
	processes, err := watchedProcessDescendants(owner, owner, nil, nil, maxWatchedDescendants)
	baseline := make(map[int]uint64, len(processes))
	for _, process := range processes {
		baseline[process.pid] = process.started
	}
	return baseline, err
}

func configureWatchedCommandStart(*syscall.SysProcAttr) {}

func lockWatchedCommandStart() func() { return func() {} }

func waitWatchedCommandStart(*exec.Cmd) error { return nil }

func resumeWatchedCommand(*exec.Cmd) error { return nil }

func startWatchedProcessTracking(tracker *watchedProcessTracker) error {
	events := make(chan os.Signal, 1)
	stopped := make(chan struct{})
	done := make(chan struct{})
	signal.Notify(events, syscall.SIGCHLD)
	tracker.reapAdopted()
	tracker.stop = func() {
		signal.Stop(events)
		close(stopped)
		<-done
	}
	go func() {
		defer close(done)
		for {
			select {
			case <-events:
				tracker.reapAdopted()
			case <-stopped:
				return
			}
		}
	}()
	return nil
}

func (tracker *watchedProcessTracker) reapAdopted() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if err := tracker.refreshLocked(); err != nil && tracker.eventErr == nil {
		tracker.eventErr = err
	}
	for pid, started := range tracker.processes {
		process, ok := watchedProcess(pid)
		if !ok || process.started != started || process.parent != tracker.owner {
			continue
		}
		var status syscall.WaitStatus
		waited, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		if waited == pid || errors.Is(err, syscall.ECHILD) {
			delete(tracker.processes, pid)
		}
	}
}

func finishWatchedProcessTracking(tracker *watchedProcessTracker) {
	_ = tracker.drainEvents()
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		tracker.reapAdopted()
		if tracker.processCount() == 0 {
			return
		}
		_ = tracker.signal(syscall.SIGKILL)
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func watchedProcessDescendants(owner, root int, tracked, excluded map[int]uint64, limit int) ([]watchedProcessInfo, error) {
	queue := make([]watchedProcessInfo, 0, len(tracked)+2)
	seen := make(map[int]bool, len(tracked)+2)
	for _, pid := range []int{owner, root} {
		if process, ok := watchedProcess(pid); ok && !seen[pid] {
			seen[pid] = true
			queue = append(queue, process)
		}
	}
	processes := make([]watchedProcessInfo, 0, len(tracked))
	for pid, started := range tracked {
		process, ok := watchedProcess(pid)
		if !ok || process.started != started || seen[pid] || excluded[pid] == process.started {
			continue
		}
		seen[pid] = true
		processes = append(processes, process)
		queue = append(queue, process)
	}
	for len(queue) != 0 {
		parent := queue[0]
		queue = queue[1:]
		current, ok := watchedProcess(parent.pid)
		if !ok || current.started != parent.started {
			continue
		}
		tasks, err := watchedProcessTasks(parent.pid, maxWatchedThreads)
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
				process, exists := watchedProcess(pid)
				if !exists || process.parent != parent.pid {
					continue
				}
				if excluded[pid] == process.started {
					seen[pid] = true
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

func watchedProcessTasks(pid, limit int) ([]int, error) {
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
		task, parseErr := strconv.Atoi(name)
		if parseErr == nil {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func watchedProcess(pid int) (watchedProcessInfo, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return watchedProcessInfo{}, false
	}
	close := strings.LastIndex(string(data), ") ")
	if close < 0 {
		return watchedProcessInfo{}, false
	}
	fields := strings.Fields(string(data[close+2:]))
	if len(fields) <= 19 {
		return watchedProcessInfo{}, false
	}
	parent, parentErr := strconv.Atoi(fields[1])
	group, groupErr := strconv.Atoi(fields[2])
	started, startedErr := strconv.ParseUint(fields[19], 10, 64)
	if parentErr != nil || groupErr != nil || startedErr != nil {
		return watchedProcessInfo{}, false
	}
	return watchedProcessInfo{pid: pid, parent: parent, group: group, started: started}, true
}

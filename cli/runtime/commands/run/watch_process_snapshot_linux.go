//go:build linux

package run

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func prepareWatchedProcessTracking() error {
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}

func configureWatchedCommandStart(*syscall.SysProcAttr) {}

func lockWatchedCommandStart() func() { return func() {} }

func waitWatchedCommandStart(*exec.Cmd) error { return nil }

func resumeWatchedCommand(*exec.Cmd) error { return nil }

func startWatchedProcessTracking(*watchedProcessTracker) error {
	return nil
}

func finishWatchedProcessTracking(tracker *watchedProcessTracker) {
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		for {
			var status syscall.WaitStatus
			pid, _ := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
			if pid <= 0 {
				break
			}
		}
		if !trackedLinuxProcessRemains(tracker) || time.Now().After(deadline) {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func trackedLinuxProcessRemains(tracker *watchedProcessTracker) bool {
	if tracker == nil {
		return false
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	for pid, started := range tracker.processes {
		if process, ok := watchedProcess(pid); ok && started == process.started {
			return true
		}
	}
	return false
}

func watchedProcessDescendants(owner, root int, tracked map[int]uint64, limit int) ([]watchedProcessInfo, error) {
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
		if !ok || process.started != started || seen[pid] {
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
		file, err := os.Open("/proc/" + strconv.Itoa(parent.pid) + "/task/" + strconv.Itoa(parent.pid) + "/children")
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return processes, err
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
	return processes, nil
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

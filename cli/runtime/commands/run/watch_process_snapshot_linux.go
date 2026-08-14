//go:build linux

package run

import (
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
	snapshot, err := watchedProcessSnapshot()
	if err != nil {
		return false
	}
	for _, process := range snapshot {
		if started, ok := tracker.processes[process.pid]; ok && started == process.started {
			return true
		}
	}
	return false
}

func watchedProcessSnapshot() ([]watchedProcessInfo, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	processes := make([]watchedProcessInfo, 0, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if process, ok := watchedProcess(pid); ok {
			processes = append(processes, process)
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

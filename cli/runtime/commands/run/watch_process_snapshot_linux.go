//go:build linux

package run

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func prepareWatchedProcessTracking() error {
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}

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
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		close := strings.LastIndex(string(data), ") ")
		if close < 0 {
			continue
		}
		fields := strings.Fields(string(data[close+2:]))
		if len(fields) <= 19 {
			continue
		}
		parent, parentErr := strconv.Atoi(fields[1])
		group, groupErr := strconv.Atoi(fields[2])
		started, startedErr := strconv.ParseUint(fields[19], 10, 64)
		if parentErr != nil || groupErr != nil || startedErr != nil {
			continue
		}
		processes = append(processes, watchedProcessInfo{pid: pid, parent: parent, group: group, started: started})
	}
	return processes, nil
}

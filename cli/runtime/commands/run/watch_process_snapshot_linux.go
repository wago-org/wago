//go:build linux

package run

import (
	"os"
	"strconv"
	"strings"
)

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
		started, startedErr := strconv.ParseUint(fields[19], 10, 64)
		if parentErr != nil || startedErr != nil {
			continue
		}
		processes = append(processes, watchedProcessInfo{pid: pid, parent: parent, started: started})
	}
	return processes, nil
}

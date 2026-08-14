//go:build darwin

package run

import "golang.org/x/sys/unix"

func watchedProcessSnapshot() ([]watchedProcessInfo, error) {
	entries, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	processes := make([]watchedProcessInfo, 0, len(entries))
	for _, entry := range entries {
		started := uint64(entry.Proc.P_starttime.Sec)*1_000_000 + uint64(entry.Proc.P_starttime.Usec)
		processes = append(processes, watchedProcessInfo{
			pid:     int(entry.Proc.P_pid),
			parent:  int(entry.Eproc.Ppid),
			started: started,
		})
	}
	return processes, nil
}

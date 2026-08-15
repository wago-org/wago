//go:build !linux

package watchsupervisor

// Enter is unavailable outside Linux.
func Enter() {}

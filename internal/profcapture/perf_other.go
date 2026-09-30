//go:build !linux

package profcapture

import "fmt"

func control(string, string, string) error { return fmt.Errorf("perf collection requires Linux") }
func RecordPerf(Options, []string) error {
	return fmt.Errorf("perf collection requires Linux; use none for metadata or pprof for Go CPU sampling")
}

//go:build linux

package compiler

import "testing"

func TestCPUInfoBrandPreference(t *testing.T) {
	contents := "processor : 0\nHardware : fallback board\nmodel name : AMD EPYC 9R14\n"
	if got := cpuInfoBrand(contents); got != "AMD EPYC 9R14" {
		t.Fatalf("cpuInfoBrand = %q", got)
	}
}

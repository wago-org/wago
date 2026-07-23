//go:build amd64

package amd64

import (
	"os"
	"strings"
	"testing"
)

func TestHostSupportsAVX512(t *testing.T) {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		t.Skip(err)
	}
	flags := string(data)
	want := strings.Contains(flags, " avx512f ") && strings.Contains(flags, " avx512dq ") &&
		strings.Contains(flags, " avx512bw ")
	if got := hostSupportsAVX512(); got != want {
		t.Fatalf("hostSupportsAVX512=%v, cpuinfo avx512f+bw=%v", got, want)
	}
}

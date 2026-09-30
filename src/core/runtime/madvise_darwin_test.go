//go:build darwin && arm64 && !tinygo

package runtime

import (
	"os/exec"
	"testing"
)

func TestReusedMemoryAfterExec(t *testing.T) {
	const size = 1 << 20
	m, err := AcquireJobMemoryGrowable(size, size)
	if err != nil {
		t.Fatal(err)
	}
	b := m.CurrentBytes()
	for i := range b {
		b[i] = 0xa5
	}
	if err := exec.Command("/usr/bin/true").Run(); err != nil {
		t.Fatal(err)
	}
	original := m
	if err := ReleaseJobMemory(original); err != nil {
		t.Fatal(err)
	}
	m, err = AcquireJobMemoryGrowable(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m != original {
		t.Fatal("expected the original JobMemory to be reused")
	}

	for i, b := range m.CurrentBytes() {
		if b != 0 {
			t.Fatalf("reused linear memory byte %d = %#x, want 0", i, b)
		}
	}
}

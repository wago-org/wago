//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"sync/atomic"
	"testing"
)

func TestSealingMappedCodeDoesNotLeaveAnExecutableRange(t *testing.T) {
	code, _, err := MapCode([]byte{0xc3})
	if err != nil {
		t.Fatal(err)
	}
	start := slicePtr(code)
	if err := SealCode(code); err != nil {
		_ = Unmap(code)
		t.Fatal(err)
	}
	if err := Unmap(code); err != nil {
		t.Fatal(err)
	}
	executableCodeMu.Lock()
	defer executableCodeMu.Unlock()
	for i := range executableCodeRanges {
		if atomic.LoadUintptr(&executableCodeRanges[i].start) == start {
			t.Fatal("unmapped code remains in the executable range registry")
		}
	}
}

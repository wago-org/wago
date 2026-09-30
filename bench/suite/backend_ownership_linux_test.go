//go:build linux

package wagobench

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	core "github.com/wago-org/wago/src/core/runtime"
)

func benchAddressMapped(t *testing.T, addr uintptr) bool {
	t.Helper()
	f, err := os.Open("/proc/self/maps")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		var lo, hi uint64
		if _, err := fmt.Sscanf(strings.Fields(s.Text())[0], "%x-%x", &lo, &hi); err != nil {
			t.Fatal(err)
		}
		if uint64(addr) >= lo && uint64(addr) < hi {
			return true
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

func TestBenchNativeMappingRelease(t *testing.T) {
	m, err := wasm.DecodeModule(fibWasm)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		cm, err := benchCompileModule(m)
		if err != nil {
			t.Fatal(err)
		}
		owner, ok := cm.image.(*core.CodeBuffer)
		if !ok {
			_ = cm.Close()
			t.Fatal("missing native owner")
		}
		addr := owner.Base()
		mapped := benchAddressMapped(t, addr)
		if err := cm.Close(); err != nil {
			t.Fatal(err)
		}
		if !mapped || benchAddressMapped(t, addr) {
			t.Fatalf("iteration %d: native mapping ownership mismatch", i)
		}
	}
}

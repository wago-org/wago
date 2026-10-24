//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"strings"
	"testing"
)

func TestRetiredPoliciesRejectPerCompileOverrides(t *testing.T) {
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, []byte{0, 0x41, 1, 0x0b})
	for _, name := range []string{"guarded-test-ccmp", "streaming-i32-reduction", "dot-loop-vector", "crowded-products", "linear-integer-tree", "loop-trap-cell", "prepared-light-entry"} {
		for _, enabled := range []bool{false, true} {
			_, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{name: enabled}})
			if err == nil || !strings.Contains(err.Error(), "unknown arm64 optimization") {
				t.Fatalf("%s=%v: %v", name, enabled, err)
			}
		}
	}
}

//go:build linux && amd64

package amd64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCompileRejectsCompleteFrameAboveStackFenceHeadroom(t *testing.T) {
	results := make([]wasm.ValType, 8192)
	for i := range results {
		results[i] = wasm.V128
	}
	body := append([]byte{0x01}, wasmtest.ULEB(8192)...)
	body = append(body, 0x7b, 0x10, 0x00, 0x0b) // v128 locals; call self; end
	m := mod1(t, nil, results, body)
	if _, err := CompileModule(m); err == nil || !strings.Contains(err.Error(), "exceeds stack-fence headroom") {
		t.Fatalf("complete-frame compile error = %v", err)
	}
}

//go:build linux && amd64

package amd64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCompileRejectsFrameLargerThanStackFenceHeadroom(t *testing.T) {
	body := append([]byte{0x01}, wasmtest.ULEB(40_000)...)
	body = append(body, 0x7b, 0x0b) // v128 local run, end
	m := mod1(t, nil, nil, body)
	if _, err := CompileModule(m); err == nil || !strings.Contains(err.Error(), "exceeds stack-fence headroom") {
		t.Fatalf("oversized native frame compile error = %v", err)
	}
}

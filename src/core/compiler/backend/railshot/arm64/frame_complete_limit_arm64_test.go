//go:build arm64

package arm64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCompileRejectsCompleteFrameAboveStackFenceHeadroom(t *testing.T) {
	body := append([]byte{0x01}, wasmtest.ULEB(32768)...)
	body = append(body, 0x7e, 0x10, 0x00, 0x0b) // i64 locals; call self; end
	m := mod1(t, nil, nil, body)
	if _, err := CompileModule(m); err == nil || !strings.Contains(err.Error(), "exceeds stack-fence headroom") {
		t.Fatalf("complete-frame compile error = %v", err)
	}
}

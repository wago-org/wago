//go:build arm64

package arm64

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestDumpLZ4Native dumps the generated ARM64 native code for the LZ4 corpus
// module to /tmp/lz4_func%d_wago.dis so the failing store base (0x1ffff8) can be
// located in the generated code.
func TestDumpLZ4Native(t *testing.T) {
	wasmBytes, err := os.ReadFile("/Users/work/Code/Wago/wago-semantic-corpus/tests/corpora/lz4/lz4.wasm")
	if err != nil {
		t.Skipf("lz4.wasm not present: %v", err)
	}
	mod, err := wasm.DecodeModule(wasmBytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	cm, err := CompileModule(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Dump each function's native bytes to its own file.
	for i := range mod.Code {
		start := cm.Entry[i]
		end := len(cm.Code)
		if i+1 < len(mod.Code) {
			end = cm.Entry[i+1]
		}
		out := make([]byte, 0, end-start)
		out = append(out, cm.Code[start:end]...)
		// Pad to 16 bytes for a clean objdump.
		for len(out)%16 != 0 {
			out = append(out, 0)
		}
		fname := "/tmp/lz4_func" + itoa(i) + "_wago.bin"
		if err := os.WriteFile(fname, out, 0644); err != nil {
			t.Fatalf("write %s: %v", fname, err)
		}
		t.Logf("func %d: entry=%d len=%d -> %s", i, cm.Entry[i], end-start, fname)
	}
	// Also dump the full image.
	os.WriteFile("/tmp/lz4_full_wago.bin", cm.Code, 0644)
	// And the entry offsets for func[2].
	_ = binary.LittleEndian
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

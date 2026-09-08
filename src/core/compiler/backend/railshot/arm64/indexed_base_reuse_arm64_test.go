//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func indexedBaseReuseModuleArm64(t testing.TB) *wasm.Module {
	// Eight adjacent loads from one address make the function dense enough for
	// folded indexed displacement. Assign every value so each deferred load is
	// materialized before the next access; the final result is the eighth word.
	body := []byte{0x01, 0x01, 0x7f}
	for off := byte(0); off < 32; off += 4 {
		body = append(body, 0x20, 0x00, 0x28, 0x02, off, 0x21, 0x01)
	}
	body = append(body, 0x20, 0x01, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestIndexedBaseReuseSwitchAndExecutionArm64(t *testing.T) {
	m := indexedBaseReuseModuleArm64(t)
	compile := func(on bool) *CodegenStats {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{
			"indexed-base-reuse": on,
			"load-pair":          false,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		return stats.Funcs[0]
	}
	on, off := compile(true), compile(false)
	run := func(on bool) uint32 {
		saved := indexedBaseReuseEnabled
		indexedBaseReuseEnabled = on
		defer func() { indexedBaseReuseEnabled = saved }()
		got, err := runArm64WrapperMem(t, m, 0, func(mem []byte) {
			for i := 0; i < 8; i++ {
				binary.LittleEndian.PutUint32(mem[i*4:], uint32(i+1))
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	gotOn, gotOff := run(true), run(false)
	if gotOn != 8 || gotOff != gotOn {
		t.Fatalf("results enabled/disabled = %d/%d, want 8/8", gotOn, gotOff)
	}
	if hits := on.Peephole["indexed-base-reuse"]; hits == 0 {
		t.Fatalf("indexed base reuse did not fire (all: %v)", on.Peephole)
	}
	if off.Peephole["indexed-base-reuse"] != 0 || on.CodeBytes >= off.CodeBytes {
		t.Fatalf("enabled code/hits = %d/%d, disabled = %d/%d", on.CodeBytes, on.Peephole["indexed-base-reuse"], off.CodeBytes, off.Peephole["indexed-base-reuse"])
	}
}

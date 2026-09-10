//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCompactLoopAlign32PolicyAMD64(t *testing.T) {
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I64}, body: []byte{
		0x01, 0x03, 0x7e, // three i64 locals
		0x42, 0x00, 0x21, 0x01,
		0x42, 0x01, 0x21, 0x02,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x00, 0x45, 0x0d, 0x01,
		0x20, 0x01, 0x20, 0x02, 0x7c, 0x21, 0x03,
		0x20, 0x02, 0x21, 0x01,
		0x20, 0x03, 0x21, 0x02,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00,
		0x0c, 0x00, 0x0b, 0x0b,
		0x20, 0x01, 0x0b,
	}})
	compile := func(on bool) ([]byte, *CodegenStats) {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Workers:       1,
			Stats:         &stats,
			Optimizations: map[string]bool{"compact-loop-align32": on},
		})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		return append([]byte(nil), cm.Code...), stats.Funcs[0]
	}

	offCode, off := compile(false)
	onCode, on := compile(true)
	if bytes.Equal(offCode, onCode) {
		t.Fatal("32-byte compact loop alignment did not change code")
	}
	if got := off.Peephole["compact-loop-align32"]; got != 0 {
		t.Fatalf("disabled compact-loop-align32 hits = %d", got)
	}
	if got := on.Peephole["compact-loop-align32"]; got != 1 {
		t.Fatalf("enabled compact-loop-align32 hits = %d, want 1", got)
	}
}

func TestCompactLoopAlign32RejectsLargeFunctionsAMD64(t *testing.T) {
	body := []byte{0x00} // no locals
	for range 64 {
		body = append(body, 0x01) // nop
	}
	body = append(body, 0x03, 0x40, 0x0b, 0x0b)
	m := modFuncs(t, funcDef{body: body})
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Workers:       1,
		Stats:         &stats,
		Optimizations: map[string]bool{"compact-loop-align32": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Peephole["compact-loop-align32"]; got != 0 {
		t.Fatalf("large-function compact-loop-align32 hits = %d", got)
	}
}

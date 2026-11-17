//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestMemoryLoadIndexUXTWFoldsNoncanonicalAddressArm64(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x00,
		0x41, 4, 0x41, 42, 0x36, 0x02, 0x00, // store 42 at address 4
		0x20, 0x00, 0x28, 0x02, 0x00, // reload param0
		0x0b,
	})
	compile := func(enabled bool) (*CodegenStats, []byte) {
		t.Helper()
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true, Stats: &stats, Optimizations: map[string]bool{
			"uxtw-add":       enabled,
			"value-facts":    false,
			"store-load-fwd": false,
			"store-forward":  false,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			t.Cleanup(func() { _ = cm.CodeImage.Close() })
		}
		return stats.Funcs[0], append([]byte(nil), cm.Code...)
	}
	on, onCode := compile(true)
	off, _ := compile(false)
	if got := on.Peephole["memory-load-index-uxtw"]; got != 1 {
		t.Fatalf("UXTW selections = %d, want 1; peeps=%v", got, on.Peephole)
	}
	if got := off.Peephole["memory-load-index-uxtw"]; got != 0 {
		t.Fatalf("disabled UXTW selections = %d, want 0", got)
	}
	if on.CodeBytes != off.CodeBytes {
		t.Fatalf("optimized code = %d bytes, rollback = %d", on.CodeBytes, off.CodeBytes)
	}
	var indexed int
	for at := 0; at+4 <= len(onCode); at += 4 {
		word := binary.LittleEndian.Uint32(onCode[at:])
		if word&0x3b20fc00 == 0x38204800 {
			indexed++
		}
	}
	if indexed < 1 {
		t.Fatalf("UXTW memory instructions = %d, want at least 1", indexed)
	}
	for _, enabled := range []bool{false, true} {
		opts := CompileOptions{ElideBoundsChecks: true, Optimizations: map[string]bool{
			"uxtw-add":       enabled,
			"store-load-fwd": false,
			"store-forward":  false,
		}}
		if got, err := runArm64WrapperWithOptions(t, m, opts, 0x8877665500000004); err != nil || got != 42 {
			t.Fatalf("enabled=%v: got=%d err=%v, want 42", enabled, got, err)
		}
	}
}

func TestCanonicalAddressLoadsPreserveValuesAndInvalidationArm64(t *testing.T) {
	for _, update := range []bool{false, true} {
		for _, barrier := range []bool{false, true} {
			body := []byte{1, 2, 0x7f}
			for _, entry := range []struct{ address, value int32 }{{0, 0x11223344}, {4, 16}, {8, -1430532899}, {12, 24}} {
				body = append(body, 0x41)
				body = append(body, wasmtest.SLEB32(entry.address)...)
				body = append(body, 0x41)
				body = append(body, wasmtest.SLEB32(entry.value)...)
				body = append(body, 0x36, 2, 0)
			}
			body = append(body, 0x20, 0, 0x21, 1, 0x20, 2, 0x20, 1, 0x28, 2, 0, 0x6a, 0x21, 2)
			if barrier {
				body = append(body, 0x20, 0, 0x45, 0x04, 0x40, 0x01, 0x05, 0x01, 0x0b)
			}
			if update {
				body = append(body, 0x20, 1, 0x41, 8, 0x6a, 0x21, 1)
			}
			body = append(body, 0x20, 1, 0x28, 2, 4, 0x21, 1, 0x20, 2, 0x20, 1, 0x73, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, reuse := range []bool{false, true} {
				for _, facts := range []bool{false, true} {
					for _, bounds := range []bool{false, true} {
						for _, input := range []uint64{0, 8, 0x8877665500000000, 0x8877665500000008} {
							var value, next uint32
							if uint32(input) == 0 {
								value, next = 0x11223344, 16
								if update {
									next = 24
								}
							} else {
								value, next = 0xaabbccdd, 24
								if update {
									next = 0
								}
							}
							want := value ^ next
							var stats ModuleStats
							opts := CompileOptions{NoBoundsFacts: !bounds, Optimizations: map[string]bool{"indexed-base-reuse": reuse, "value-facts": facts}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, input)
							if err != nil || got != uint64(want) {
								t.Fatalf("update=%v reuse=%v facts=%v bounds=%v input=%x: got=%x want=%x err=%v", update, reuse, facts, bounds, input, got, want, err)
							}
							if diagnosticsEnabled {
								hits := stats.Funcs[0].Peephole["memory-address-canonical-load"]
								admitted := facts
								if (hits != 0) != admitted {
									t.Fatalf("update=%v reuse=%v facts=%v bounds=%v: selections=%d", update, reuse, facts, bounds, hits)
								}
							}
						}
						if _, err := runArm64WrapperWithOptions(t, m, CompileOptions{NoBoundsFacts: !bounds, Optimizations: map[string]bool{"indexed-base-reuse": reuse, "value-facts": facts}}, 65532); err == nil {
							t.Fatal("cross-boundary load did not trap")
						}
					}
				}
			}
		}
	}
}

//go:build linux && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLazyIntervalBorrowsPreserveCode(t *testing.T) {
	saved := lazyIntervalBorrowsEnabled
	defer func() { lazyIntervalBorrowsEnabled = saved }()
	for _, control := range []bool{false, true} {
		body := intervalRegionBody()
		if control {
			const split = 3 + 20*4
			next := append([]byte(nil), body[:split]...)
			next = append(next, 0x20, 0, 0x04, 0x40, 0x20, 1, 0x1a, 0x0b)
			body = append(next, body[split:]...)
		}
		m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
		for _, signals := range []bool{false, true} {
			var baseline []byte
			for _, lazy := range []bool{false, true} {
				lazyIntervalBorrowsEnabled = lazy
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, ElideBoundsChecks: signals, Workers: 1, Stats: optionalTestStats(&stats)})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				if diagnosticsEnabled {
					if stats.Funcs[0].Peephole["interval-region"] == 0 {
						t.Fatal("fixture did not use regional locals")
					}
				}
				if !lazy {
					baseline = append([]byte(nil), cm.Code...)
				} else if !bytes.Equal(baseline, cm.Code) {
					t.Fatalf("code changed: control=%t signals=%t", control, signals)
				}
				if got := runCompiledAmd64u(t, cm); got != 210 {
					t.Fatalf("control=%t signals=%t lazy=%t: result=%d", control, signals, lazy, got)
				}
			}
		}
	}
}

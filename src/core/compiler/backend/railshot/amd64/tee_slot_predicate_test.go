//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTeeSlotPredicateExecution(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := teeSlotPredicateEnabled
	defer func() { teeSlotPredicateEnabled = old }()
	for _, consumer := range []string{"if", "br_if", "drop"} {
		for _, prefix := range []bool{false, true} {
			body := []byte{1, 1, 0x7f}
			if prefix {
				body = append(body, 0x41, 23)
			}
			if consumer == "br_if" {
				body = append(body, 0x02, 0x40)
			}
			// A join returns the input in its canonical slot. Tee updates a live local
			// and leaves the same value for the next control-flow consumer.
			body = append(body, 0x20, 0, 0x04, 0x7f, 0x20, 0, 0x05, 0x41, 0, 0x0b, 0x22, 1)
			switch consumer {
			case "if":
				body = append(body, 0x04, 0x40, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0b)
			case "br_if":
				body = append(body, 0x0d, 0, 0x20, 1, 0x41, 2, 0x6a, 0x21, 1, 0x0b)
			case "drop":
				body = append(body, 0x1a)
			}
			body = append(body, 0x20, 1)
			if prefix {
				body = append(body, 0x6a)
			}
			body = append(body, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, enabled := range []bool{false, true} {
				for _, compact := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/prefix%v/enabled%v/compact%v", consumer, prefix, enabled, compact), func(t *testing.T) {
						teeSlotPredicateEnabled = enabled
						var stats ModuleStats
						cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, CompactNative: compact, Optimizations: map[string]bool{"reg-merge": false, "interval-control": false}})
						if err != nil {
							t.Fatal(err)
						}
						defer cm.CodeImage.Close()
						wantAdmission := enabled && consumer != "drop"
						if got := stats.Funcs[0].Peephole["tee-slot-predicate"] != 0; got != wantAdmission {
							t.Fatalf("admission=%v want=%v pins=%d", got, wantAdmission, stats.Funcs[0].PinnedLocals)
						}
						for _, input := range []uint32{0, 1, 0x7fffffff, 0xffffffff} {
							want := input
							if consumer == "if" && input != 0 {
								want++
							}
							if consumer == "br_if" && input == 0 {
								want += 2
							}
							if prefix {
								want += 23
							}
							if got := uint32(runCompiledAmd64u(t, cm, uint64(input))); got != want {
								t.Fatalf("input=%x got=%x want=%x", input, got, want)
							}
						}
					})
				}
			}
		}
	}
}

func TestTeeSlotPredicateBackedge(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := teeSlotPredicateEnabled
	defer func() { teeSlotPredicateEnabled = old }()
	for _, prefix := range []bool{false, true} {
		body := []byte{1, 1, 0x7f}
		if prefix {
			body = append(body, 0x41, 19)
		}
		body = append(body,
			0x03, 0x40,
			0x20, 1, 0x41, 1, 0x6a, 0x21, 1,
			0x20, 0, 0x04, 0x7f, 0x20, 0, 0x41, 1, 0x6b, 0x05, 0x41, 0, 0x0b,
			0x22, 0, 0x0d, 0, 0x0b, 0x20, 1)
		if prefix {
			body = append(body, 0x6a)
		}
		body = append(body, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, enabled := range []bool{false, true} {
			for _, compact := range []bool{false, true} {
				t.Run(fmt.Sprintf("prefix%v/enabled%v/compact%v", prefix, enabled, compact), func(t *testing.T) {
					teeSlotPredicateEnabled = enabled
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, CompactNative: compact, Optimizations: map[string]bool{"reg-merge": false, "interval-control": false}})
					if err != nil {
						t.Fatal(err)
					}
					defer cm.CodeImage.Close()
					if got := stats.Funcs[0].Peephole["tee-slot-predicate"] != 0; got != enabled {
						t.Fatalf("admission=%v enabled=%v", got, enabled)
					}
					for _, n := range []uint64{0, 1, 2, 7, 23} {
						want := max(n, 1)
						if prefix {
							want += 19
						}
						if got := runCompiledAmd64u(t, cm, n); got != want {
							t.Fatalf("n=%d got=%d want=%d", n, got, want)
						}
					}
				})
			}
		}
	}
}

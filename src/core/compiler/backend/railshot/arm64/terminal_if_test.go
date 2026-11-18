//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestTerminalIfRestoresDirtyAndSpilledLocalsArm64(t *testing.T) {
	saved := inlineEnabled
	inlineEnabled = false
	t.Cleanup(func() { inlineEnabled = saved })
	for _, beforeCall := range []bool{false, true} {
		for _, terminal := range []bool{false, true} {
			body := []byte{1, 1, 0x7f, 0x20, 1, 0x41, 3, 0x6a, 0x21, 3}
			if beforeCall {
				body = append(body, 0x20, 1, 0x10, 1, 0x1a)
			}
			body = append(body, 0x20, 0, 0x04, 0x7f,
				0x20, 1, 0x10, 1, 0x1a,
				0x20, 2, 0x41, 5, 0x6a, 0x21, 1,
				0x20, 1, 0x20, 3, 0x6a,
				0x05, 0x20, 1, 0x10, 1, 0x20, 2, 0x6a, 0x20, 3, 0x6a, 0x0b)
			if !terminal {
				body = append(body, 0x41, 1, 0x6a)
			}
			body = append(body, 0x0b)
			m := modFuncs(t,
				funcDef{params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body},
				funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x0b}},
			)
			for _, enabled := range []bool{false, true} {
				for _, cond := range []uint64{0, 1, 17} {
					for _, pair := range [][2]uint64{{7, 5}, {0xffffffff, 0x88776655}, {0x12345678, 0xffffffff}} {
						a, b := uint32(pair[0]), uint32(pair[1])
						want := a + b + 8
						if cond == 0 {
							want = 2*a + b + 3
						}
						if !terminal {
							want++
						}
						var stats ModuleStats
						opts := CompileOptions{Optimizations: map[string]bool{"merge-next-use": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, cond, pair[0], pair[1])
						if err != nil || got != uint64(want) {
							t.Fatalf("precall=%v terminal=%v enabled=%v cond=%d args=%x: got=%x want=%x err=%v", beforeCall, terminal, enabled, cond, pair, got, want, err)
						}
						if diagnosticsEnabled {
							wantHits := 0
							if enabled && terminal {
								wantHits = 1
							}
							if hits := stats.Funcs[0].Peephole["terminal-if-local-stores-elide"]; hits != wantHits {
								t.Fatalf("terminal=%v enabled=%v: selections=%d want=%d", terminal, enabled, hits, wantHits)
							}
						}
					}
				}
			}
		}
	}
}

func TestTerminalCallDeadLocalsPreserveStackAndLaterReadsArm64(t *testing.T) {
	body := []byte{1, 1, 0x7f, 0x20, 3, 0x41, 7, 0x6a, 0x21, 4,
		0x20, 0, 0x04, 0x7f,
		0x20, 1, 0x20, 2, 0x6a, 0x20, 3, 0x10, 1, 0x6a, 0x20, 4, 0x6a,
		0x05, 0x20, 1, 0x20, 2, 0x73, 0x20, 3, 0x10, 1, 0x6a, 0x20, 4, 0x6a, 0x0b, 0x0b}
	// A callee that uses and clobbers pinned locals, rather than a pin-preserving
	// identity, makes omitted stores observable if either lifetime proof is wrong.
	callee := []byte{1, 1, 0x7f, 0x20, 0, 0x41, 3, 0x6c, 0x21, 1, 0x20, 1, 0x0b}
	m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body}, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: callee})
	for _, enabled := range []bool{false, true} {
		for _, cond := range []uint64{0, 1} {
			for _, seed := range []uint64{7, 0x88776655, 0xffffffff} {
				a, b, c := uint32(seed), uint32(seed+3), uint32(seed+11)
				want := a + b
				if cond == 0 {
					want = a ^ b
				}
				want += c*3 + c + 7
				var stats ModuleStats
				opts := CompileOptions{Optimizations: map[string]bool{"inline": false, "merge-next-use": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, cond, uint64(a), uint64(b), uint64(c))
				if err != nil || got != uint64(want) {
					t.Fatalf("enabled=%v cond=%d seed=%x: got=%x want=%x err=%v", enabled, cond, seed, got, want, err)
				}
				if diagnosticsEnabled {
					hits := stats.Funcs[0].Peephole["terminal-call-local-store-elide"]
					if (hits != 0) != enabled {
						t.Fatalf("enabled=%v: selections=%d", enabled, hits)
					}
				}
			}
		}
	}
}

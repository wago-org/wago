//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Keep ten mutable local values live through eight-argument calls, including
// repeated and reordered arguments, a deferred operand below the arguments,
// and memory traffic. Mixed calls also materialize a late FP literal.
func TestGuardCallPinPreservesArgumentsAndLocals(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := guardCallPinEnabled
	defer func() { guardCallPinEnabled = old }()
	for _, mixed := range []bool{false, true} {
		callerTypes := []wasm.ValType{wasm.I64, wasm.I64, wasm.I64, wasm.I64}
		helperTypes := make([]wasm.ValType, 8)
		for i := range helperTypes {
			helperTypes[i] = wasm.I64
		}
		body := []byte{1, 6, 0x7e}
		for i := 0; i < 10; i++ {
			body = append(body, 0x20, byte(i%4), 0x42, byte(i+17), 0x85, 0x21, byte(i))
		}
		body = append(body, 0x41, 0, 0x20, 9, 0x37, 3, 0, 0x41, 0, 0x29, 3, 0, 0x21, 9)
		// A deferred expression below the call arguments exercises flushBelow.
		body = append(body, 0x20, 8, 0x20, 9, 0x85)
		indices := [8]byte{9, 7, 0, 5, 0, 3, 2, 1}
		for i, x := range indices {
			body = append(body, 0x20, x, 0x42, byte(i+33), 0x85)
		}
		const literal = uint64(0x7ff8123456789abc)
		helper := []byte{0, 0x42, 0}
		if mixed {
			helperTypes = append(helperTypes, wasm.F64)
			body = append(body, 0x44)
			body = binary.LittleEndian.AppendUint64(body, literal)
			helper = []byte{0, 0x20, 8, 0xbd}
		}
		for i := 0; i < 8; i++ {
			helper = append(helper, 0x20, byte(i), 0x42, byte(i+1), 0x89, 0x85)
		}
		helper = append(helper, 0x0b)
		body = append(body, 0x10, 1, 0x85)
		for i := 0; i < 10; i++ {
			body = append(body, 0x20, byte(i), 0x42, byte(i+1), 0x89, 0x85)
		}
		body = append(body, 0x0b)
		m := modFuncs(t, funcDef{callerTypes, []wasm.ValType{wasm.I64}, body}, funcDef{helperTypes, []wasm.ValType{wasm.I64}, helper})
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		for _, enabled := range []bool{false, true} {
			for _, lazy := range []bool{false, true} {
				for _, pool := range []bool{false, true} {
					t.Run(fmt.Sprintf("mixed%v/enabled%v/lazy%v/pool%v", mixed, enabled, lazy, pool), func(t *testing.T) {
						guardCallPinEnabled = enabled
						var stats ModuleStats
						cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true, Stats: &stats, Optimizations: map[string]bool{"inline": false, "stack-reg": lazy, "interval-control": false, "v128-const-cache": pool}})
						if err != nil {
							t.Fatal(err)
						}
						defer cm.CodeImage.Close()
						admitted := stats.Funcs[0].Peephole["guard-call-pin"] != 0
						if admitted != enabled {
							t.Fatalf("admission=%v enabled=%v pins=%d", admitted, enabled, stats.Funcs[0].PinnedLocals)
						}
						for _, seed := range []uint64{0, 1, 0x123456789abcdef0, ^uint64(0)} {
							args := make([]uint64, 4)
							var locals [10]uint64
							for i := range args {
								args[i] = bits.RotateLeft64(seed, i*7) ^ uint64(i)*0x0102030405060708
								locals[i] = args[i]
							}
							for i := range locals {
								locals[i] = locals[i%4] ^ uint64(i+17)
							}
							want := locals[8] ^ locals[9]
							if mixed {
								want ^= literal
							}
							for i, x := range indices {
								want ^= bits.RotateLeft64(locals[x]^uint64(i+33), i+1)
							}
							for i, x := range locals {
								want ^= bits.RotateLeft64(x, i+1)
							}
							if got := runCompiledAmd64u(t, cm, args...); got != want {
								t.Fatalf("seed=%x got=%x want=%x", seed, got, want)
							}
						}
					})
				}
			}
		}
	}
}

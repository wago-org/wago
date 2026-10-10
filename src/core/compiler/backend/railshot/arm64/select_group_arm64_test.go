//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func appendGroupUpdates(body []byte, destination, mask, input byte, spread bool, trapLast ...bool) []byte {
	for i := uint32(0); i < 6; i++ {
		body = append(body, 0x20, input)
		if len(trapLast) > 0 && trapLast[0] && i == 5 {
			body = append(body, 0x28, 2, 0)
		}
		body = append(body, 0x73, 0x41)
		body = append(body, wasmtest.SLEB32(int32(0x10203041+i*2))...)
		body = append(body, 0x6c, 0x41)
		body = append(body, wasmtest.SLEB32(int32(i*23+3))...)
		body = append(body, 0x6a, 0x41)
		body = append(body, wasmtest.SLEB32(int32(0x45671235-i*4))...)
		body = append(body, 0x6c, 0x20, destination, 0x20, mask, 0x41)
		bit := i
		if spread {
			bit = i * 5
		}
		body = append(body, wasmtest.SLEB32(int32(uint32(1)<<bit))...)
		body = append(body, 0x71, 0x1b)
		if i == 5 {
			body = append(body, 0x21, destination)
		} else {
			body = append(body, 0x22, destination)
		}
	}
	return body
}

func groupOracle(mask, input, accumulator uint32, spread bool) uint32 {
	for i := uint32(0); i < 6; i++ {
		bit := i
		if spread {
			bit = i * 5
		}
		if mask&(uint32(1)<<bit) != 0 {
			accumulator = ((accumulator^input)*(0x10203041+i*2) + (i*23 + 3)) * (0x45671235 - i*4)
		}
	}
	return accumulator
}

func selectGroupModule(t testing.TB, prefix bool, trapLast ...bool) *wasm.Module {
	body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a}
	if prefix {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(0x34567812)...)
	}
	body = append(body, 0x20, 1, 0x22, 2)
	body = appendGroupUpdates(body, 2, 0, 1, false, trapLast...)
	body = append(body, 0x20, 2)
	if prefix {
		body = append(body, 0x73)
	}
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func TestSelectGroupMasksAndPrefix(t *testing.T) {
	saved := selectGroupEnabled
	defer func() { selectGroupEnabled = saved }()
	for _, prefix := range []bool{false, true} {
		m := selectGroupModule(t, prefix)
		for _, enabled := range []bool{false, true} {
			selectGroupEnabled = enabled
			if diagnosticsEnabled {
				stats := &ModuleStats{}
				cm, err := CompileModuleWith(m, CompileOptions{Stats: stats})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					cm.CodeImage.Close()
				}
				if (stats.Funcs[0].Peephole["select-group-guard"] != 0) != enabled {
					t.Fatalf("prefix=%v enabled=%v stats=%v", prefix, enabled, stats.Funcs[0].Peephole)
				}
			}
			for mask := uint32(0); mask < 64; mask++ {
				for _, input := range []uint32{0x12345678, 0xffffffff} {
					want := groupOracle(mask, input, input, false)
					if prefix {
						want ^= 0x34567812
					}
					got := runArm64u(t, m, uint64(mask), uint64(input))
					if got != uint64(want) {
						t.Fatalf("prefix=%v enabled=%v mask=%x input=%x got=%x want=%x", prefix, enabled, mask, input, got, want)
					}
				}
			}
		}
	}
}

func selectGroupLoopModule(t testing.TB, mode string, spread bool, minimal bool) *wasm.Module {
	body := []byte{1, 3, 0x7f, 0x3f, 0, 0x1a, 0x41}
	body = append(body, wasmtest.SLEB32(65536)...)
	body = append(body, 0x21, 2, 0x20, 1, 0x21, 3, 0x20, 0, 0x21, 4)
	if mode == "alternating" {
		body = append(body, 0x41, 0, 0x21, 4)
	}
	body = append(body, 0x03, 0x40)
	switch mode {
	case "zero":
		body = append(body, 0x41, 0, 0x21, 4)
	case "full":
		body = append(body, 0x41, 0x7f, 0x21, 4)
	case "alternating":
		body = append(body, 0x20, 4, 0x41, 0x7f, 0x73, 0x21, 4)
	case "random":
		for _, shift := range []byte{13, 17, 5} {
			op := byte(0x74)
			if shift == 17 {
				op = 0x76
			}
			body = append(body, 0x20, 4, 0x20, 4, 0x41, shift, op, 0x73, 0x21, 4)
		}
	}
	body = append(body, 0x20, 3, 0x22, 3)
	if minimal {
		body = appendMinimalGroupUpdates(body, spread)
	} else {
		body = appendGroupUpdates(body, 3, 4, 1, spread)
	}
	body = append(body, 0x20, 2, 0x41, 1, 0x6b, 0x22, 2, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m
}

func BenchmarkSelectGroupConditions(b *testing.B)        { benchmarkSelectGroups(b, false) }
func BenchmarkSelectGroupMinimalConditions(b *testing.B) { benchmarkSelectGroups(b, true) }
func benchmarkSelectGroups(b *testing.B, minimal bool) {
	saved := selectGroupEnabled
	defer func() { selectGroupEnabled = saved }()
	for _, mode := range []string{"zero", "full", "alternating", "random"} {
		for _, spread := range []bool{false, true} {
			label := mode + "/contiguous"
			if spread {
				label = mode + "/spread"
			}
			m := selectGroupLoopModule(b, mode, spread, minimal)
			mask, accumulator := uint32(0x76543210), uint32(0x12345679)
			if mode == "alternating" {
				mask = 0
			}
			for i := 0; i < 65536; i++ {
				switch mode {
				case "zero":
					mask = 0
				case "full":
					mask = ^uint32(0)
				case "alternating":
					mask = ^mask
				case "random":
					mask ^= mask << 13
					mask ^= mask >> 17
					mask ^= mask << 5
				}
				if minimal {
					for bit := uint32(0); bit < 6; bit++ {
						position := bit
						if spread {
							position *= 5
						}
						if mask&(uint32(1)<<position) != 0 {
							accumulator *= 0x12345679
						}
					}
				} else {
					accumulator = groupOracle(mask, 0x12345679, accumulator, spread)
				}
			}
			for _, enabled := range []bool{false, true} {
				name := label + "/eager"
				if enabled {
					name = label + "/guarded"
				}
				b.Run(name, func(b *testing.B) {
					selectGroupEnabled = enabled
					cm, err := CompileModule(m)
					if err != nil {
						b.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					eng, err := coreruntime.NewEngine()
					if err != nil {
						b.Fatal(err)
					}
					defer eng.Close()
					memory, err := coreruntime.NewJobMemory(65536)
					if err != nil {
						b.Fatal(err)
					}
					defer memory.Close()
					arena, err := coreruntime.NewArena(4096)
					if err != nil {
						b.Fatal(err)
					}
					defer arena.Close()
					code, base, err := coreruntime.MapCode(cm.Code)
					if err != nil {
						b.Fatal(err)
					}
					defer coreruntime.Unmap(code)
					args, results, trap := arena.Alloc(16), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
					binary.LittleEndian.PutUint64(args, 0x76543210)
					binary.LittleEndian.PutUint64(args[8:], 0x12345679)
					check := func() {
						if err := eng.Call(base+uintptr(cm.Entry[0]), args, memory.LinearMemory(), trap, results); err != nil {
							b.Fatal(err)
						}
						if got := uint32(binary.LittleEndian.Uint64(results)); got != accumulator {
							b.Fatalf("got=%x want=%x", got, accumulator)
						}
					}
					check()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						check()
					}
				})
			}
		}
	}
}

func appendMinimalGroupUpdates(body []byte, spread bool) []byte {
	for i := uint32(0); i < 6; i++ {
		body = append(body, 0x20, 1, 0x6c, 0x20, 3, 0x20, 4, 0x41)
		bit := i
		if spread {
			bit *= 5
		}
		body = append(body, wasmtest.SLEB32(int32(uint32(1)<<bit))...)
		body = append(body, 0x71, 0x1b)
		if i == 5 {
			body = append(body, 0x21, 3)
		} else {
			body = append(body, 0x22, 3)
		}
	}
	return body
}

func TestSelectGroupRetainsEagerLoadTrap(t *testing.T) {
	saved := selectGroupEnabled
	defer func() { selectGroupEnabled = saved }()
	m := selectGroupModule(t, false, true)
	for _, enabled := range []bool{false, true} {
		selectGroupEnabled = enabled
		for _, address := range []uint64{16, 65533, 0xffffffff} {
			got, err := runArm64Wrapper(t, m, 0, address)
			if address > 65532 {
				if err == nil {
					t.Fatalf("enabled=%v address=%x: eager load trap lost", enabled, address)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if got != address {
					t.Fatalf("enabled=%v zero mask changed accumulator: got=%x want=%x", enabled, got, address)
				}
			}
		}
	}
}

func TestSelectGroupPerCompilePolicyAndDirtyCarrier(t *testing.T) {
	m := selectGroupModule(t, false)
	for _, setting := range []struct{ facts, group bool }{{true, false}, {true, true}, {false, true}} {
		opts := CompileOptions{Optimizations: map[string]bool{"value-facts": setting.facts, "select-group-guard": setting.group}}
		for _, mask := range []uint64{0, 0x1234567800000000, 0x123456780000003f} {
			input := uint64(0xfedcba9834567812)
			got, err := runArm64WrapperWithOptions(t, m, opts, mask, input)
			if err != nil {
				t.Fatal(err)
			}
			want := groupOracle(uint32(mask), uint32(input), uint32(input), false)
			if got != uint64(want) {
				t.Fatalf("settings=%v mask=%x got=%x want=%x", setting, mask, got, want)
			}
		}
	}
}

func TestSelectGroupRejectsChangedMask(t *testing.T) {
	body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a, 0x20, 1, 0x22, 2, 0x20, 0, 0x41, 1, 0x73, 0x21, 0}
	body = appendGroupUpdates(body, 2, 0, 1, false)
	body = append(body, 0x20, 2, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	opts := CompileOptions{Optimizations: map[string]bool{"select-group-guard": true}}
	if diagnosticsEnabled {
		stats := &ModuleStats{}
		opts.Stats = stats
		cm, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		if stats.Funcs[0].Peephole["select-group-guard"] != 0 {
			t.Fatalf("changed mask group was admitted: %v", stats.Funcs[0].Peephole)
		}
		opts.Stats = nil
	}
	for _, mask := range []uint32{0, 1, 0x3f, 0xffffffff} {
		input := uint32(0x13243546)
		got, err := runArm64WrapperWithOptions(t, m, opts, uint64(mask), uint64(input))
		if err != nil {
			t.Fatal(err)
		}
		want := groupOracle(mask^1, input, input, false)
		if got != uint64(want) {
			t.Fatalf("mask=%x got=%x want=%x", mask, got, want)
		}
	}
}

func TestSelectGroupFullWordGuard(t *testing.T) {
	body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a, 0x20, 1, 0x22, 2}
	masks := []uint32{1, 2, 4, 8, 16, 0xffffffe0}
	for i, mask := range masks {
		body = append(body, 0x20, 1, 0x73, 0x41)
		body = append(body, wasmtest.SLEB32(int32(0x10203041+i*2))...)
		body = append(body, 0x6c, 0x20, 2, 0x20, 0, 0x41)
		body = append(body, wasmtest.SLEB32(int32(mask))...)
		body = append(body, 0x71, 0x1b)
		if i == 5 {
			body = append(body, 0x21, 2)
		} else {
			body = append(body, 0x22, 2)
		}
	}
	body = append(body, 0x20, 2, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	for _, zeroBranch := range []bool{false, true} {
		opts := CompileOptions{Optimizations: map[string]bool{"select-group-guard": true, "zero-branch": zeroBranch}}
		for _, mask := range []uint32{0, 1, 32, 0x80000000, 0xffffffff} {
			input := uint32(0x34567812)
			want := input
			for i, bits := range masks {
				if mask&bits != 0 {
					want = (want ^ input) * uint32(0x10203041+i*2)
				}
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, uint64(mask), uint64(input))
			if err != nil {
				t.Fatal(err)
			}
			if got != uint64(want) {
				t.Fatalf("zeroBranch=%v mask=%x got=%x want=%x", zeroBranch, mask, got, want)
			}
		}
	}
}

func TestSelectGroupLazyZeroMask(t *testing.T) {
	body := []byte{1, 2, 0x7f, 0x3f, 0, 0x1a, 0x20, 0, 0x22, 1}
	body = appendGroupUpdates(body, 1, 2, 0, false)
	body = append(body, 0x20, 1, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	for _, group := range []bool{false, true} {
		opts := CompileOptions{Optimizations: map[string]bool{"select-group-guard": group}}
		input := uint64(0x12345678)
		got, err := runArm64WrapperWithOptions(t, m, opts, input)
		if err != nil {
			t.Fatal(err)
		}
		if got != input {
			t.Fatalf("group=%v got=%x want=%x", group, got, input)
		}
	}
}

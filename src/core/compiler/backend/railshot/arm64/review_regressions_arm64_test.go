//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDominatedAddressAfterGuardedTestFold(t *testing.T) {
	// Keep a bad native rewrite from taking down every test in this package.
	// The parent still observes the exact process failure as a test failure.
	if os.Getenv("WAGO_ADDRESS_REUSE_CRASH_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDominatedAddressAfterGuardedTestFold$")
		cmd.Env = append(os.Environ(), "WAGO_ADDRESS_REUSE_CRASH_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated address-reuse execution failed: %v\n%s", err, out)
		}
		return
	}
	body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a, 0x20, 0, 0x41}
	body = append(body, wasmtest.SLEB32(65520)...)
	body = append(body, 0x71, 0x21, 4)
	for _, store := range [][2]byte{{12, 9}, {16, 9}, {20, 9}, {24, 9}, {4, 11}, {8, 42}} {
		body = append(body, 0x20, 4, 0x41, store[1], 0x36, 2, store[0])
	}
	body = append(body, 0x02, 0x40, 0x02, 0x40,
		0x20, 1, 0x20, 3, 0x71, 0x0d, 0,
		0x20, 2, 0x20, 1, 0x41, 0x7f, 0x73, 0x71, 0x45, 0x0d, 1,
		0x0b, 0x20, 4, 0x28, 2, 4, 0x0f, 0x0b, 0x20, 4, 0x28, 2, 4, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, bounds := range []bool{false, true} {
		for _, fold := range []bool{false, true} {
			for _, a := range []uint64{0, 1} {
				t.Run(fmt.Sprintf("bounds%v/fold%v/a%d", bounds, fold, a), func(t *testing.T) {
					var stats ModuleStats
					opts := CompileOptions{ElideBoundsChecks: bounds, Optimizations: map[string]bool{"guarded-test-ccmp": fold}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, 64, a, 2, 1)
					if err != nil || got != 11 {
						t.Fatalf("got %d, %v; want 11", got, err)
					}
					if diagnosticsEnabled && fold && stats.Funcs[0].Peephole["ccmp-guarded-tests"] == 0 {
						t.Fatalf("fold not exercised: %v", stats.Funcs[0].Peephole)
					}
				})
			}
		}
	}
}

func TestIntervalBranchFlushPreservesDirtyLocals(t *testing.T) {
	for _, depth := range []int{6, 8, 10} {
		body := []byte{1, 80, 0x7f, 0x3f, 0, 0x1a, 0x02, 0x40}
		for x := byte(1); x <= 28; x++ {
			body = append(body, 0x41, x, 0x21, x, 0x20, x, 0x1a, 0x20, x, 0x1a)
		}
		var tree func(int)
		tree = func(d int) {
			if d == 0 {
				body = append(body, 0x20, 0)
				return
			}
			tree(d - 1)
			tree(d - 1)
			body = append(body, 0x6a)
		}
		tree(depth)
		body = append(body, 0x0d, 0)
		for x := byte(1); x <= 28; x++ {
			body = append(body, 0x41, 0, 0x21, x)
		}
		body = append(body, 0x0b, 0x41, 0)
		for x := byte(1); x <= 28; x++ {
			body = append(body, 0x20, x, 0x6a)
		}
		body = append(body, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		for _, enabled := range []bool{false, true} {
			for _, taken := range []uint64{0, 1} {
				t.Run(fmt.Sprintf("depth%d/enabled%v/taken%d", depth, enabled, taken), func(t *testing.T) {
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"interval-region-pins": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, taken)
					want := uint64(406) * taken
					if err != nil || got != want {
						t.Fatalf("got %d, %v; want %d", got, err, want)
					}
					if diagnosticsEnabled && enabled && stats.Funcs[0].Residency.Evictions == 0 {
						t.Fatal("pressure eviction not exercised")
					}
				})
			}
		}
	}
}

func TestIntervalReferenceBranchPreservesDirtyLocals(t *testing.T) {
	for _, op := range []byte{0xd5, 0xd6} {
		blockType := byte(0x40)
		if op == 0xd6 {
			blockType = 0x6e // br_on_non_null supplies an anyref to the block.
		}
		// Build the reference before activating regional locals, so reference
		// selection's ordinary control edges do not flush away the reproducer.
		body := []byte{2, 80, 0x7f, 1, 0x6e, 0x3f, 0, 0x1a,
			0x20, 0, 0x04, 0x6e, 0xd0, 0x6e, 0x05, 0x41, 0, 0xfb, 0x1c, 0x0b,
			0x21, 81, 0x02, blockType}
		for x := byte(1); x <= 28; x++ {
			body = append(body, 0x41, x, 0x21, x, 0x20, x, 0x1a, 0x20, x, 0x1a)
		}
		body = append(body, 0x20, 81, op, 0)
		if op == 0xd5 {
			body = append(body, 0x1a) // non-null fallthrough retains the reference.
		}
		for x := byte(1); x <= 28; x++ {
			body = append(body, 0x41, 0, 0x21, x)
		}
		if op == 0xd6 {
			body = append(body, 0xd0, 0x6e) // fallthrough block result.
		}
		body = append(body, 0x0b)
		if op == 0xd6 {
			body = append(body, 0x1a)
		}
		body = append(body, 0x41, 0)
		for x := byte(1); x <= 28; x++ {
			body = append(body, 0x20, x, 0x6a)
		}
		body = append(body, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"default", "enabled", "disabled"} {
			for _, null := range []uint64{0, 1} {
				t.Run(fmt.Sprintf("op%x/%s/null%d", op, mode, null), func(t *testing.T) {
					var stats ModuleStats
					opts := CompileOptions{}
					if mode != "default" {
						opts.Optimizations = map[string]bool{"interval-region-pins": mode == "enabled"}
					}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, null)
					want := uint64(0)
					if op == 0xd5 && null == 1 || op == 0xd6 && null == 0 {
						want = 406
					}
					if err != nil || got != want {
						t.Fatalf("got %d, %v; want %d", got, err, want)
					}
					if diagnosticsEnabled && mode != "disabled" && stats.Funcs[0].Peephole["interval-region"] == 0 {
						t.Fatalf("regional caching not exercised: %v", stats.Funcs[0].Peephole)
					}
				})
			}
		}
	}
}

func TestDotLoopPreservesDiscardedLoadTrap(t *testing.T) {
	for _, address := range []int32{0, 65536} {
		m := dotLoopModule(t, 2, 0, 64, 0, 0)
		prefix := append([]byte{0x41}, wasmtest.SLEB32(address)...)
		prefix = append(prefix, 0x28, 2, 0, 0x21, 3)
		body := m.Code[0].BodyBytes
		pos := bytes.Index(body, []byte{0x03, 0x40}) + 2
		m.Code[0].BodyBytes = append(append(append([]byte{}, body[:pos]...), prefix...), body[pos:]...)
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("address%d/enabled%v", address, enabled), func(t *testing.T) {
				var stats ModuleStats
				opts := CompileOptions{Optimizations: map[string]bool{"dot-loop-vector": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, 0)
				if address == 65536 {
					if err == nil {
						t.Fatalf("discarded load must trap, got %x", got)
					}
				} else if err != nil || got != 0xfffff7b1 {
					t.Fatalf("got %x, %v", got, err)
				}
				if diagnosticsEnabled && stats.Funcs[0].Peephole["dot-loop-vector"] != 0 {
					t.Fatal("extra load admitted")
				}
			})
		}
	}
}

func TestMinPlusCachedPivotOverlap(t *testing.T) {
	for _, limit := range []uint32{24, 40, 72, 136} {
		for _, pivot := range []uint32{0, 16, 32, 64} {
			m := minPlusModule(t, 0, 256, pivot, 0, limit)
			body := m.Code[0].BodyBytes
			m.Code[0].Locals.Runs[0].Count += 2
			for i := byte(7); i <= 8; i++ {
				body = bytes.Replace(body, []byte{0x20, 2, 0x28, 2, 0}, []byte{0x20, i}, 1)
			}
			pos := bytes.Index(body, []byte{0x03, 0x40}) + 2
			prefix := []byte{0x20, 2, 0x28, 2, 0, 0x21, 7, 0x20, 2, 0x28, 2, 0, 0x21, 8}
			m.Code[0].BodyBytes = append(append(append([]byte{}, body[:pos]...), prefix...), body[pos:]...)
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			original := make([]byte, 65536)
			for i := uint32(0); i < 256; i += 4 {
				binary.LittleEndian.PutUint32(original[i:], 100)
			}
			// Lower the first word in the packet that aliases the pivot.
			binary.LittleEndian.PutUint32(original[256+(pivot&^7):], 0xfffffff6)
			expected := append([]byte{}, original...)
			var lastOld, lastSum, lastAddr uint32
			for c := uint32(0); c < limit; c += 8 {
				cached := binary.LittleEndian.Uint32(expected[pivot:])
				for off := uint32(0); off < 8; off += 4 {
					lastAddr = c + off
					lastOld = binary.LittleEndian.Uint32(expected[lastAddr:])
					lastSum = binary.LittleEndian.Uint32(expected[256+lastAddr:]) + cached
					v := lastSum
					if int32(lastOld) < int32(v) {
						v = lastOld
					}
					binary.LittleEndian.PutUint32(expected[lastAddr:], v)
				}
			}
			want := limit ^ lastAddr ^ lastOld ^ lastSum ^ 123
			for i := uint32(0); i < limit; i += 4 {
				want = (want + binary.LittleEndian.Uint32(expected[i:])) * 0x9e3779b9
			}
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("limit%d/pivot%d/enabled%v", limit, pivot, enabled), func(t *testing.T) {
					opts := CompileOptions{Optimizations: map[string]bool{"dot-loop-vector": enabled}}
					got, err := runArm64WrapperMemWithOptions(t, m, opts, 0, func(mem []byte) { copy(mem, original) })
					if err != nil || got != want {
						t.Fatalf("got %x, %v; want %x", got, err, want)
					}
				})
			}
		}
	}
}

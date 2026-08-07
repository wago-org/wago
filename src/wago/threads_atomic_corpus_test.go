//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

func TestThreadsOfficialAtomicCoreExecutesWithinImportedMemoryBoundary(t *testing.T) {
	dir := filepath.Clean("../../tests/regressions/spectest-proposals/threads")
	original, err := os.ReadFile(filepath.Join(dir, "atomic.0.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	adapted, err := adaptOfficialAtomicCoreModule(original)
	if err != nil {
		t.Fatal(err)
	}
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, adapted)
	if err != nil {
		t.Fatalf("compile adapted official atomic core: %v", err)
	}
	defer compiled.Close()
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	instance, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	raw, err := os.ReadFile(filepath.Join(dir, "atomic.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture proposalFixtureFile
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	modules, actions, returns, traps := 0, 0, 0, 0
	for _, command := range fixture.Commands {
		if command.Type == "module" {
			modules++
			if modules > 1 {
				break
			}
			continue
		}
		if modules != 1 {
			continue
		}
		args := make([]uint64, len(command.Action.Args))
		for i, value := range command.Action.Args {
			args[i], err = proposalDecimalBits(value.Value, map[string]int{"i32": 32, "i64": 64}[value.Type])
			if err != nil {
				t.Fatalf("line %d argument %d: %v", command.Line, i, err)
			}
		}
		if command.Type == "action" && command.Action.Field == "init" {
			if len(args) != 1 {
				t.Fatalf("line %d init arity = %d", command.Line, len(args))
			}
			binary.LittleEndian.PutUint64(memory.Bytes()[:8], args[0])
			actions++
			continue
		}
		results, invokeErr := instance.Invoke(command.Action.Field, args...)
		switch command.Type {
		case "assert_return":
			if invokeErr != nil {
				t.Fatalf("line %d %s: %v", command.Line, command.Action.Field, invokeErr)
			}
			if len(results) != len(command.Expected) {
				t.Fatalf("line %d %s result count = %d, want %d", command.Line, command.Action.Field, len(results), len(command.Expected))
			}
			for i, expected := range command.Expected {
				width := map[string]int{"i32": 32, "i64": 64}[expected.Type]
				want, parseErr := proposalDecimalBits(expected.Value, width)
				if parseErr != nil {
					t.Fatalf("line %d expected result %d: %v", command.Line, i, parseErr)
				}
				if width == 32 {
					want = uint64(uint32(want))
				}
				if results[i] != want {
					t.Fatalf("line %d %s result %d = %#x, want %#x", command.Line, command.Action.Field, i, results[i], want)
				}
			}
			returns++
		case "assert_trap":
			if invokeErr == nil || !proposalNegativeFailureMatches(command.Text, invokeErr) {
				t.Fatalf("line %d %s = %v, want trap %q", command.Line, command.Action.Field, invokeErr, command.Text)
			}
			traps++
		default:
			t.Fatalf("line %d unexpected command %q in first official module", command.Line, command.Type)
		}
	}
	if actions != 63 || returns != 149 || traps != 45 {
		t.Fatalf("official atomic accounting = actions %d returns %d traps %d, want 63/149/45", actions, returns, traps)
	}
}

func TestThreadsOfficialAtomicWaitNotifyExecutesWithinImportedMemoryBoundary(t *testing.T) {
	dir := filepath.Clean("../../tests/regressions/spectest-proposals/threads")
	original, err := os.ReadFile(filepath.Join(dir, "atomic.1.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	adapted, err := adaptOfficialAtomicCoreModule(original)
	if err != nil {
		t.Fatal(err)
	}
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(config, adapted)
	if err != nil {
		t.Fatalf("compile adapted official wait/notify module: %v", err)
	}
	defer compiled.Close()
	memory, _ := NewSharedMemory(1, 1)
	defer memory.Close()
	instance, err := Instantiate(compiled, Imports{"env.memory": memory})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	binary.LittleEndian.PutUint64(memory.Bytes()[:8], 0xffffffffffff)

	for _, tc := range []struct {
		name string
		args []uint64
		want uint64
	}{
		{"memory.atomic.wait32", []uint64{0, 0, 0}, 1},
		{"memory.atomic.wait64", []uint64{0, 0, 0}, 1},
		{"memory.atomic.notify", []uint64{0, 0}, 0},
	} {
		out, err := instance.Invoke(tc.name, tc.args...)
		if err != nil || len(out) != 1 || out[0] != tc.want {
			t.Errorf("%s = %v, %v; want %d", tc.name, out, err, tc.want)
		}
	}
	for _, tc := range []struct {
		name string
		args []uint64
		want string
	}{
		{"memory.atomic.wait32", []uint64{65536, 0, 0}, "out of bounds memory access"},
		{"memory.atomic.wait64", []uint64{65536, 0, 0}, "out of bounds memory access"},
		{"memory.atomic.notify", []uint64{65536, 0}, "out of bounds memory access"},
		{"memory.atomic.wait32", []uint64{65531, 0, 0}, "unaligned atomic"},
		{"memory.atomic.wait64", []uint64{65524, 0, 0}, "unaligned atomic"},
		{"memory.atomic.notify", []uint64{65531, 0}, "unaligned atomic"},
	} {
		_, err := instance.Invoke(tc.name, tc.args...)
		if err == nil || !proposalNegativeFailureMatches(tc.want, err) {
			t.Errorf("%s = %v, want %q", tc.name, err, tc.want)
		}
	}
}

type rawWasmSection struct {
	id      byte
	payload []byte
}

// adaptOfficialAtomicCoreModule keeps every official atomic function body and
// assertion intact while fitting the first Wago product boundary: its local
// shared memory becomes an imported shared memory, and the suite's ordinary
// i64.store initializer becomes two drops plus a nop. The test performs that
// initialization through the host-owned memory before replaying each assertion.
func adaptOfficialAtomicCoreModule(module []byte) ([]byte, error) {
	if len(module) < 8 || !bytes.Equal(module[:8], []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}) {
		return nil, fmt.Errorf("invalid Wasm header")
	}
	var sections []rawWasmSection
	for off := 8; off < len(module); {
		id := module[off]
		off++
		size, n, err := readTestULEB(module[off:])
		if err != nil {
			return nil, err
		}
		off += n
		if size > uint64(len(module)-off) {
			return nil, fmt.Errorf("section %d is truncated", id)
		}
		sections = append(sections, rawWasmSection{id: id, payload: append([]byte(nil), module[off:off+int(size)]...)})
		off += int(size)
	}
	var memoryPayload []byte
	for _, section := range sections {
		if section.id == 2 {
			return nil, fmt.Errorf("official module unexpectedly already imports values")
		}
		if section.id == 5 {
			memoryPayload = section.payload
		}
	}
	if !bytes.Equal(memoryPayload, []byte{0x01, 0x03, 0x01, 0x01}) {
		return nil, fmt.Errorf("official memory payload = %x, want one shared 1/1 memory", memoryPayload)
	}
	entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	entry = append(entry, 0x02)
	entry = append(entry, memoryPayload[1:]...)
	importSection := wasmtest.Section(2, wasmtest.Vec(entry))
	out := append([]byte(nil), module[:8]...)
	inserted := false
	for _, section := range sections {
		if section.id == 5 {
			continue
		}
		out = append(out, wasmtest.Section(section.id, section.payload)...)
		if section.id == 1 {
			out = append(out, importSection...)
			inserted = true
		}
	}
	if !inserted {
		return nil, fmt.Errorf("official module has no type section")
	}
	from := []byte{0x41, 0x00, 0x20, 0x00, 0x37, 0x03, 0x00, 0x0b}
	to := []byte{0x41, 0x00, 0x20, 0x00, 0x1a, 0x1a, 0x01, 0x0b}
	if bytes.Count(out, from) != 1 {
		return nil, fmt.Errorf("official initializer signature count = %d, want 1", bytes.Count(out, from))
	}
	return bytes.Replace(out, from, to, 1), nil
}

func readTestULEB(data []byte) (uint64, int, error) {
	var value uint64
	for i, b := range data {
		if i >= 10 || i == 9 && b > 1 {
			return 0, 0, fmt.Errorf("ULEB overflow")
		}
		value |= uint64(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return value, i + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("truncated ULEB")
}

//go:build linux && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestSegueScalarMemoryCoverage is a static phase-0 control for #918. It does
// not manipulate GS; Go/runtime entry and exit safety remains unproved.
func TestSegueScalarMemoryCoverage(t *testing.T) {
	aux, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		t.Fatal(err)
	}
	var hwcap2 uint64
	for i := 0; i+16 <= len(aux); i += 16 {
		if binary.LittleEndian.Uint64(aux[i:]) == 26 {
			hwcap2 = binary.LittleEndian.Uint64(aux[i+8:])
			break
		}
	}
	t.Logf("kernel_fsgsbase_enabled=%v hwcap2=%#x", hwcap2&2 != 0, hwcap2)
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/semantic/yyjson/yyjson.wasm",
		"corpus/workloads/compute/sha256.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/micropython/micropython.wasm",
		"corpus/workloads/synthetic/memory_tree.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModuleWithFeatures(data, wasm.ValidationFeatures{})
			if err != nil {
				t.Fatal(err)
			}
			importsMemory := false
			for _, imp := range m.Imports {
				if imp.Type.Kind == wasm.ExternMem {
					importsMemory = true
				}
			}
			eligibleShape := len(m.Memories) == 1 && !importsMemory && !m.Memories[0].Shared && !m.Memories[0].Limits.Addr64
			var loads, stores, wideOffset int
			classify := wasm.NewModuleInstructionClassifier(m, true)
			var imm wasm.InstructionImmediate
			for _, f := range m.Code {
				r := wasm.NewReader(f.BodyBytes)
				for r.HasNext() {
					op, err := r.Byte()
					if err != nil {
						t.Fatal(err)
					}
					if err := classify.ClassifyInto(r, op, &imm); err != nil {
						t.Fatal(err)
					}
					if op >= 0x28 && op <= 0x35 {
						loads++
					} else if op >= 0x36 && op <= 0x3e {
						stores++
					} else {
						continue
					}
					if imm.MemOffset > 0x7fffffff {
						wideOffset++
					}
				}
			}
			t.Logf("owned_unshared_memory32=%v imported_memory=%v memories=%d scalar_loads=%d scalar_stores=%d offsets_over_int32=%d", eligibleShape, importsMemory, len(m.Memories), loads, stores, wideOffset)
		})
	}
}

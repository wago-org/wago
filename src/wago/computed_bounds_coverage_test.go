//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type computedBoundsKey struct {
	local  uint32
	add    int32
	offset uint64
	width  byte
}

// TestComputedBoundsSourceUpperBound measures one exact, easy-to-prove source
// shape. This precedes backend pin/fold/certificate selection and is therefore
// only an upper bound for this shape, not a count of eliminated checks.
func TestComputedBoundsSourceUpperBound(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
		"corpus/workloads/semantic/yyjson/yyjson.wasm",
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
			loads, forms, reversedForms, repeats, err := countComputedBoundsSource(m)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("scalar_loads=%d local_plus_constant_load_forms=%d reversed_operand_forms=%d exact_repeats_upper_bound=%d", loads, forms, reversedForms, repeats)
		})
	}
}

func countComputedBoundsSource(m *wasm.Module) (loads, forms, reversedForms, repeats int, err error) {
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var imm wasm.InstructionImmediate
	for _, fn := range m.Code {
		var stage uint8
		var local uint32
		var add int32
		prior := make(map[computedBoundsKey]bool)
		r := wasm.NewReader(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return 0, 0, 0, 0, err
			}
			if op == 0x41 {
				v, err := r.I32()
				if err != nil {
					return 0, 0, 0, 0, err
				}
				add = v
				if stage == 1 || stage == 5 {
					stage = 2
				} else {
					stage = 4
				}
				continue
			}
			if err := classifier.ClassifyInto(r, op, &imm); err != nil {
				return 0, 0, 0, 0, err
			}
			if op == 0x20 {
				local = imm.Index
				if stage == 4 {
					stage = 5
				} else {
					stage = 1
				}
				continue
			}
			if op == 0x6a && (stage == 2 || stage == 5) {
				if stage == 2 {
					stage = 3
				} else {
					stage = 6
				}
				continue
			}
			if op >= 0x28 && op <= 0x35 {
				loads++
				if (stage == 3 || stage == 6) && (!imm.HasMemIndex || imm.MemIndex == 0) {
					forms++
					if stage == 6 {
						reversedForms++
					}
					key := computedBoundsKey{local, add, imm.MemOffset, op}
					if prior[key] {
						repeats++
					}
					prior[key] = true
				}
				stage = 0
				continue
			}
			stage = 0
			// Include all supported calls/control and conservatively kill the whole
			// GC prefix. Unsupported legacy EH fails in the classifier above.
			if (op >= 0x36 && op <= 0x40) || op <= 0x15 || op == 0x1f || op == 0xd5 || op == 0xd6 || op == 0xfb || op == 0xfc || op == 0xfd || op == 0xfe || op == 0x21 || op == 0x22 {
				clear(prior)
			}
		}
	}
	return loads, forms, reversedForms, repeats, nil
}

func TestComputedBoundsSourceWindow(t *testing.T) {
	// A different key between two equal keys must not hide the later reuse.
	load := func(offset byte) []byte {
		return []byte{0x20, 0, 0x41, 4, 0x6a, 0x28, 2, offset, 0x1a}
	}
	for _, tc := range []struct {
		name    string
		barrier []byte
		want    int
	}{
		{"same_window", nil, 1},
		{"straight_line_integer_work", []byte{0x41, 0, 0x1a}, 1},
		{"call", []byte{0x10, 0}, 0},
		{"call_indirect", []byte{0x11, 0, 0}, 0},
		{"return_call", []byte{0x12, 0}, 0},
		{"return_call_indirect", []byte{0x13, 0, 0}, 0},
		{"call_ref", []byte{0x14, 0}, 0},
		{"return_call_ref", []byte{0x15, 0}, 0},
		{"try_table", []byte{0x1f, 0x40, 0}, 0},
		{"throw", []byte{0x08, 0}, 0},
		{"throw_ref", []byte{0x0a}, 0},
		{"br_on_null", []byte{0xd5, 0}, 0},
		{"br_on_non_null", []byte{0xd6, 0}, 0},
		{"br_on_cast", []byte{0xfb, 0x18, 0, 0, 0x6e, 0x6d}, 0},
		{"br_on_cast_fail", []byte{0xfb, 0x19, 0, 0, 0x6e, 0x6d}, 0},
		{"local_write", []byte{0x41, 0, 0x21, 0}, 0},
		{"store", []byte{0x41, 0, 0x41, 0, 0x36, 2, 0}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := append(load(0), load(4)...)
			body = append(body, tc.barrier...)
			body = append(body, load(0)...)
			body = append(body, 0x0b)
			m := &wasm.Module{Code: []wasm.Func{{BodyBytes: body}}}
			_, forms, _, repeats, err := countComputedBoundsSource(m)
			if err != nil || forms != 3 || repeats != tc.want {
				t.Fatalf("forms=%d repeats=%d err=%v; want 3 forms and %d repeats", forms, repeats, err, tc.want)
			}
		})
	}
}

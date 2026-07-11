package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestWazeroPortI32UpperBits ports wazero's i32_upper_bits integration test at
// c0f3a4ec. WebAssembly i32 operations must ignore dirty bits in the upper half
// of public uint64 value slots, including address and zero-divisor checks.
func TestWazeroPortI32UpperBits(t *testing.T) {
	mod := wazeroI32UpperBitsModule()
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer instance.Close()

	tests := []struct {
		name    string
		fn      string
		params  []uint64
		want    uint32
		wantErr string
	}{
		{"eqz/clean_zero", "i32_eqz", []uint64{0}, 1, ""},
		{"eqz/clean_nonzero", "i32_eqz", []uint64{42}, 0, ""},
		{"eqz/dirty_zero", "i32_eqz", []uint64{0xdeadbeef00000000}, 1, ""},
		{"eqz/dirty_nonzero", "i32_eqz", []uint64{0xdeadbeef00000001}, 0, ""},
		{"ne/same_lower_diff_upper", "i32_ne", []uint64{0xdeadbeef00000005, 0xcafebabe00000005}, 0, ""},
		{"ne/diff_lower_same_upper", "i32_ne", []uint64{0xdeadbeef00000005, 0xdeadbeef00000006}, 1, ""},
		{"ne/clean_equal", "i32_ne", []uint64{5, 5}, 0, ""},
		{"eq/same_lower_diff_upper", "i32_eq", []uint64{0xdeadbeef00000005, 0xcafebabe00000005}, 1, ""},
		{"lt_u/dirty_a_less", "i32_lt_u", []uint64{0xdeadbeef00000005, 10}, 1, ""},
		{"lt_u/dirty_b_less", "i32_lt_u", []uint64{5, 0xcafebabe0000000a}, 1, ""},
		{"lt_u/dirty_both", "i32_lt_u", []uint64{0xdeadbeef00000005, 0xcafebabe0000000a}, 1, ""},
		{"lt_u/dirty_not_less", "i32_lt_u", []uint64{0xdeadbeef0000000a, 5}, 0, ""},
		{"gt_u/dirty_a_greater", "i32_gt_u", []uint64{0xdeadbeef0000000a, 5}, 1, ""},
		{"gt_u/dirty_a_less", "i32_gt_u", []uint64{0xdeadbeef00000005, 10}, 0, ""},
		{"le_u/dirty_a_less", "i32_le_u", []uint64{0xdeadbeef00000005, 10}, 1, ""},
		{"le_u/dirty_a_equal", "i32_le_u", []uint64{0xdeadbeef0000000a, 10}, 1, ""},
		{"le_u/dirty_a_greater", "i32_le_u", []uint64{0xdeadbeef0000000b, 10}, 0, ""},
		{"ge_u/dirty_a_greater", "i32_ge_u", []uint64{0xdeadbeef0000000a, 5}, 1, ""},
		{"ge_u/dirty_a_equal", "i32_ge_u", []uint64{0xdeadbeef0000000a, 10}, 1, ""},
		{"ge_u/dirty_a_less", "i32_ge_u", []uint64{0xdeadbeef00000005, 10}, 0, ""},
		{"lt_s/dirty_neg", "i32_lt_s", []uint64{0xdeadbeeffffffffb, 1}, 1, ""},
		{"gt_s/dirty_neg", "i32_gt_s", []uint64{0xdeadbeef00000001, 0xfffffffb}, 1, ""},
		{"le_s/dirty_neg", "i32_le_s", []uint64{0xdeadbeeffffffffb, 0xfffffffb}, 1, ""},
		{"ge_s/dirty_neg", "i32_ge_s", []uint64{0xdeadbeeffffffffb, 0xfffffffb}, 1, ""},
		{"div_u/clean", "i32_div_u", []uint64{10, 3}, 3, ""},
		{"div_u/dirty_zero_divisor", "i32_div_u", []uint64{10, 0xdeadbeef00000000}, 0, "division by zero"},
		{"div_u/dirty_nonzero_divisor", "i32_div_u", []uint64{0xdeadbeef0000000a, 0xcafebabe00000003}, 3, ""},
		{"div_s/dirty_zero_divisor", "i32_div_s", []uint64{10, 0xdeadbeef00000000}, 0, "division by zero"},
		{"rem_u/clean", "i32_rem_u", []uint64{10, 3}, 1, ""},
		{"rem_u/dirty_zero_divisor", "i32_rem_u", []uint64{10, 0xdeadbeef00000000}, 0, "division by zero"},
		{"rem_s/dirty_zero_divisor", "i32_rem_s", []uint64{10, 0xdeadbeef00000000}, 0, "division by zero"},
		{"load/dirty_addr_zero", "i32_load", []uint64{0xdeadbeef00000000}, 0, ""},
		{"load/dirty_addr_valid", "i32_load", []uint64{0xdeadbeef00000064}, 0, ""},
		{"store_load/dirty_addr", "i32_store_load", []uint64{0xdeadbeef000000c8, 0x12345678}, 0x12345678, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := instance.Invoke(tt.fn, tt.params...)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got result %v", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("invoke: %v", err)
			}
			if len(got) != 1 || uint32(got[0]) != tt.want {
				t.Fatalf("result = %#x, want lower i32 %#x", got, tt.want)
			}
		})
	}

	prepared, err := instance.PrepareFunction("i32_store_load")
	if err != nil {
		t.Fatalf("prepare i32_store_load: %v", err)
	}
	got, err := prepared.Invoke(0xdeadbeef00000100, 0xcafebabe12345678)
	if err != nil {
		t.Fatalf("prepared invoke with dirty i32 slots: %v", err)
	}
	if len(got) != 1 || uint32(got[0]) != 0x12345678 {
		t.Fatalf("prepared result = %#x, want lower i32 0x12345678", got)
	}
}

func wazeroI32UpperBitsModule() []byte {
	unary := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	binary := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	names := []string{
		"i32_eqz", "i32_eq", "i32_ne", "i32_lt_u", "i32_gt_u", "i32_le_u", "i32_ge_u",
		"i32_lt_s", "i32_gt_s", "i32_le_s", "i32_ge_s", "i32_div_u", "i32_div_s",
		"i32_rem_u", "i32_rem_s", "i32_load", "i32_store_load",
	}
	opcodes := []byte{0x45, 0x46, 0x47, 0x49, 0x4b, 0x4d, 0x4f, 0x48, 0x4a, 0x4c, 0x4e, 0x6e, 0x6d, 0x70, 0x6f}

	functionTypes := make([][]byte, len(names))
	exports := make([][]byte, len(names)+1)
	codes := make([][]byte, len(names))
	for i, name := range names {
		typeIndex := uint32(1)
		body := []byte{0x20, 0x00, 0x20, 0x01}
		switch i {
		case 0, 15:
			typeIndex = 0
			body = []byte{0x20, 0x00}
		}
		switch i {
		case 15:
			body = append(body, 0x28, 0x02, 0x00)
		case 16:
			body = append(body, 0x36, 0x02, 0x00, 0x20, 0x00, 0x28, 0x02, 0x00)
		default:
			body = append(body, opcodes[i])
		}
		body = append(body, 0x0b)
		functionTypes[i] = wasmtest.ULEB(typeIndex)
		exports[i+1] = wasmtest.ExportEntry(name, 0, uint32(i))
		codes[i] = wasmtest.Code(body)
	}
	exports[0] = wasmtest.ExportEntry("memory", 2, 0)

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(unary, binary)),
		wasmtest.Section(3, wasmtest.Vec(functionTypes...)),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

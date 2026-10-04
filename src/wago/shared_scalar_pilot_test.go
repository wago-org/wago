//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func scalarPilotModule(typ wasm.ValType, body []byte, locals int) []byte {
	decl := []byte{0}
	if locals > 0 {
		decl = append([]byte{1}, wasmtest.ULEB(uint32(locals))...)
		decl = append(decl, wasm.MustEncodeValType(typ))
	}
	fn := append(decl, body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(fn))), fn...))))
}
func TestSharedScalarVersionsPressureAndJoins(t *testing.T) {
	pressure := []byte{}
	for k := 1; k <= 40; k++ {
		pressure = append(pressure, 0x20, 0, 0x41, byte(k), 0x6a, 0x22, 1)
	}
	join := append(append([]byte{}, pressure...), 0x20, 0, 0x04, 0x7f, 0x41, 7, 0x22, 1, 0x05, 0x41, 9, 0x22, 1, 0x0b)
	for k := 0; k < 40; k++ {
		join = append(join, 0x6a)
	}
	join = append(join, 0x20, 1, 0x6a, 0x0b)
	cases := []struct {
		name string
		body []byte
		want func(uint64) uint64
	}{
		{"old_version", []byte{0x20, 0, 0x41, 0x23, 0x21, 0, 0x20, 0, 0x6a, 0x0b}, func(x uint64) uint64 { return uint64(uint32(x + 35)) }},
		{"blocks_keep_deferred_versions", []byte{0x20, 0, 0x41, 2, 0x6a, 0x02, 0x7f, 0x02, 0x7f, 0x41, 7, 0x21, 0, 0x20, 0, 0x41, 3, 0x6a, 0x0b, 0x0b, 0x6a, 0x0b}, func(x uint64) uint64 { return uint64(uint32(x + 12)) }},
		{"block_around_if_join", []byte{0x20, 0, 0x02, 0x7f, 0x20, 0, 0x04, 0x7f, 0x41, 7, 0x21, 0, 0x20, 0, 0x05, 0x41, 9, 0x21, 0, 0x20, 0, 0x0b, 0x0b, 0x20, 0, 0x6a, 0x6a, 0x0b}, func(x uint64) uint64 {
			v := uint64(7)
			if uint32(x) == 0 {
				v = 9
			}
			return uint64(uint32(x + 2*v))
		}},
		{"pressure_join", join, func(x uint64) uint64 {
			v := uint64(7)
			if uint32(x) == 0 {
				v = 9
			}
			return uint64(uint32(40*x + 820 + 2*v))
		}},
		{"fixed_nested", []byte{0x20, 0, 0x20, 0, 0x41, 1, 0x6a, 0x20, 0, 0x41, 2, 0x6a, 0x74, 0x20, 0, 0x41, 3, 0x6a, 0x20, 0, 0x41, 4, 0x6a, 0x74, 0x6a, 0x6a, 0x0b}, func(x uint64) uint64 {
			v := uint32(x)
			return uint64(v + ((v + 1) << ((v + 2) & 31)) + ((v + 3) << ((v + 4) & 31)))
		}},
		{"comparison_join_overwrite", []byte{0x20, 0, 0x41, 2, 0x48, 0x41, 7, 0x21, 0, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 0, 0x0b, 0x0b}, func(x uint64) uint64 {
			if int32(x) < 2 {
				return 1
			}
			return 0
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, e := Compile(NewRuntimeConfig().WithFunctionWorkers(1), scalarPilotModule(wasm.I32, tc.body, 1))
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			in, e := Instantiate(c)
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			for _, x := range []uint64{0, 1, 3, 31, 0xffffffff, 0x80000000} {
				got, e := in.Invoke("run", x)
				if e != nil || len(got) != 1 || got[0] != tc.want(x) {
					t.Fatalf("x=%x got %v %v want %x", x, got, e, tc.want(x))
				}
			}
		})
	}
}
func TestSharedScalarI64WidthsAndReturn(t *testing.T) {
	body := []byte{0x20, 0, 0x42}
	body = append(body, wasmtest.SLEB64(-9223372036854775807)...)
	body = append(body, 0x7c, 0x22, 1, 0x20, 0, 0x42, 0x3f, 0x87, 0x85, 0x0f, 0x0b)
	c, e := Compile(nil, scalarPilotModule(wasm.I64, body, 1))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	in, e := Instantiate(c)
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	for _, x := range []uint64{0, 1, 3, 0xffffffffffffffff, 0x8000000000000000} {
		got, e := in.Invoke("run", x)
		want := (x + 0x8000000000000001) ^ uint64(int64(x)>>63)
		if e != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("x=%x got %v %v want %x", x, got, e, want)
		}
	}
}
func TestSharedScalarFallbackTrapBeforeReturn(t *testing.T) {
	// div/rem are deliberately unadmitted: the established pending-effect path
	// must report the first trap even when the return result is a constant.
	for _, opcode := range []byte{0x6d, 0x6e, 0x6f, 0x70} {
		body := []byte{0x20, 0, 0x41, 0, opcode, 0x1a, 0x41, 7, 0x0b}
		c, e := Compile(nil, scalarPilotModule(wasm.I32, body, 0))
		if e != nil {
			t.Fatal(e)
		}
		in, e := Instantiate(c)
		if e != nil {
			t.Fatal(e)
		}
		if got, e := in.Invoke("run", 3); e == nil {
			t.Fatalf("opcode=%x missing required trap: %v", opcode, got)
		}
		in.Close()
		c.Close()
	}
}

func TestSharedScalarUnpinnedLeafIngress(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		constant, add := byte(0x41), byte(0x6a)
		if wasm.EqualValType(typ, wasm.I64) {
			constant, add = 0x42, 0x7c
		}
		c, e := Compile(nil, scalarPilotModule(typ, []byte{0x20, 0, constant, 1, add, 0x0b}, 0))
		if e != nil {
			t.Fatal(e)
		}
		in, e := Instantiate(c)
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 64; i++ {
			got, e := in.Invoke("run", 41)
			if e != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("type=%v got %v %v", typ, got, e)
			}
		}
		in.Close()
		c.Close()
	}
}
func TestSharedScalarMixedBankFallback(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64, wasm.V128} {
		m := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, typ}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 0x1a, 0x20, 0, 0x41, 1, 0x6a, 0x0b}))))
		c, e := Compile(nil, m)
		if e != nil {
			t.Fatal(e)
		}
		in, e := Instantiate(c)
		if e != nil {
			t.Fatal(e)
		}
		args := []uint64{41, 0}
		if wasm.EqualValType(typ, wasm.V128) {
			args = append(args, 0)
		}
		got, e := in.Invoke("run", args...)
		if e != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("type=%v got %v %v", typ, got, e)
		}
		in.Close()
		c.Close()
	}
}

func TestSharedScalarCommutativeDeferredOperands(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, add, shl, shr := wasm.I32, byte(0x41), byte(0x6a), byte(0x74), byte(0x76)
		mask := uint64(0xffffffff)
		if wide {
			typ, constant, add, shl, shr = wasm.I64, 0x42, 0x7c, 0x86, 0x88
			mask = ^uint64(0)
		}
		for _, op := range []struct {
			i32, i64 byte
			compare  bool
			apply    func(uint64, uint64) uint64
		}{
			{0x6a, 0x7c, false, func(a, b uint64) uint64 { return a + b }},
			{0x6c, 0x7e, false, func(a, b uint64) uint64 { return a * b }},
			{0x71, 0x83, false, func(a, b uint64) uint64 { return a & b }},
			{0x72, 0x84, false, func(a, b uint64) uint64 { return a | b }},
			{0x73, 0x85, false, func(a, b uint64) uint64 { return a ^ b }},
			{0x46, 0x51, true, func(a, b uint64) uint64 {
				if a == b {
					return 1
				}
				return 0
			}},
			{0x47, 0x52, true, func(a, b uint64) uint64 {
				if a != b {
					return 1
				}
				return 0
			}},
		} {
			opcode, result := op.i32, typ
			if wide {
				opcode = op.i64
			}
			if op.compare {
				result = wasm.I32
			}
			// The deeper right tree contains fixed-register shifts. Reordering
			// must preserve the older left read and each operand's width.
			body := []byte{0x20, 0, constant, 5, add, 0x20, 0, constant, 3, shl, 0x20, 0, constant, 2, shr, add, opcode, 0x0b}
			m := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{result}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
			c, err := Compile(nil, m)
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c)
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			for _, x := range []uint64{0, 1, 3, 31, 0x80000000, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
				x &= mask
				a, b := (x+5)&mask, ((x<<3)+(x>>2))&mask
				want := op.apply(a, b) & mask
				got, err := in.Invoke("run", x)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("wide=%v op=%x x=%x: got %v %v, want %x", wide, opcode, x, got, err, want)
				}
			}
			in.Close()
			c.Close()
		}
	}
}

func TestSharedScalarPressureWithoutJoin(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		constant, add, mask := byte(0x41), byte(0x6a), uint64(0xffffffff)
		if wasm.EqualValType(typ, wasm.I64) {
			constant, add, mask = 0x42, 0x7c, ^uint64(0)
		}
		var body []byte
		for k := byte(1); k <= 40; k++ {
			body = append(body, 0x20, 0, constant, k, add, 0x22, 1)
		}
		for k := 1; k < 40; k++ {
			body = append(body, add)
		}
		body = append(body, 0x0b)
		c, err := Compile(nil, scalarPilotModule(typ, body, 1))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		for _, x := range []uint64{0, 1, 3, 0x80000000, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
			x &= mask
			got, err := in.Invoke("run", x)
			want := (40*x + 820) & mask
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("type=%v x=%x: got %v %v, want %x", typ, x, got, err, want)
			}
		}
		in.Close()
		c.Close()
	}
}

func TestSharedScalarCleanHomesAndResultRegisters(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		constant, add, ne, result, mask := byte(0x41), byte(0x6a), byte(0x47), byte(0x7f), uint64(0xffffffff)
		if wasm.EqualValType(typ, wasm.I64) {
			constant, add, ne, result, mask = 0x42, 0x7c, 0x52, 0x7e, ^uint64(0)
		}
		condition := []byte{0x20, 0, constant, 0, ne}
		// Preserve an old local version below nested if results. Both arms
		// overwrite its source home while another local still owns that value.
		body := []byte{0x20, 0, 0x22, 1}
		body = append(body, condition...)
		body = append(body, 0x04, result, constant, 7, 0x21, 0)
		body = append(body, condition...)
		body = append(body, 0x04, result, 0x20, 1, 0x05, constant, 9, 0x0b)
		body = append(body, 0x05, constant, 11, 0x21, 0, 0x20, 1, 0x0b, add, 0x20, 0, add, 0x0b)
		c, err := Compile(nil, scalarPilotModule(typ, body, 1))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		for _, x := range []uint64{0, 1, 3, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
			x &= mask
			v := uint64(7)
			if x == 0 {
				v = 11
			}
			want := (2*x + v) & mask
			got, err := in.Invoke("run", x)
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("type=%v x=%x got=%v err=%v want=%x", typ, x, got, err, want)
			}
		}
		in.Close()
		c.Close()
	}
}

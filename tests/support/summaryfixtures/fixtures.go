// Package summaryfixtures supplies bounded, immutable bytecode test inputs.
// Eighteen encodings were checked with WABT 1.0.41 --enable-all.
// The GC encoding was checked by hand; that WABT cannot parse its syntax.
package summaryfixtures

// Fixture contains the readable source and the fixed binary encoding.
type Fixture struct{ Name, WAT, Hex string }

// Modules supplies the bounded agreement matrix. Tests decode Hex
// into their own byte slices; no mutable binary storage is shared.
var Modules = []Fixture{
	{
		Name: "atomic",
		WAT:  `(module (memory 1 2 shared) (func i32.const 0 i32.atomic.load drop i32.const 0 i32.const 0 i64.const 0 memory.atomic.wait32 drop atomic.fence))`,
		Hex:  "0061736d01000000010401600000030201000504010301020a190117004100fe1002001a410041004200fe0102001afe03000b",
	},
	{
		Name: "blocks",
		WAT:  `(module (type (func (result i32 i64))) (func block (type 0) i32.const 1 i64.const 2 end drop drop block loop i32.const 0 br_if 1 br 1 end end i32.const 1 if nop else nop end))`,
		Hex:  "0061736d010000000109026000027f7e600000030201010a21011f000200410142020b1a1a0240034041000d010c010b0b410104400105010b0b",
	},
	{
		Name: "branch_table",
		WAT:  `(module (func block i32.const 0 br_table 0 0 0 0 0 0 0 0 0 end))`,
		Hex:  "0061736d01000000010401600000030201000a14011200024041000e080000000000000000000b0b",
	},
	{
		Name: "bulk_data",
		WAT:  `(module (memory 1) (data "a") (data "b") (func i32.const 0 i32.const 0 i32.const 1 memory.init 1 data.drop 0 i32.const 0 i32.const 1 i32.const 1 memory.copy i32.const 0 i32.const 0 i32.const 1 memory.fill))`,
		Hex:  "0061736d010000000104016000000302010005030100010c01020a24012200410041004101fc080100fc0900410041014101fc0a0000410041004101fc0b000b0b0702010161010162",
	},
	{
		Name: "bulk_table",
		WAT:  `(module (table 2 funcref) (elem func 0) (elem func 0) (func i32.const 0 i32.const 0 i32.const 1 table.init 1 elem.drop 0 i32.const 0 i32.const 0 i32.const 1 table.copy))`,
		Hex:  "0061736d010000000104016000000302010004040170000209090201000100010001000a1b011900410041004101fc0c0100fc0d00410041004101fc0e00000b",
	},
	{
		Name: "call_ref",
		WAT:  `(module (type (func)) (elem declare func 0) (func ref.func 0 call_ref 0))`,
		Hex:  "0061736d0100000001040160000003020100090501030001000a08010600d20014000b",
	},
	{
		Name: "calls",
		WAT:  `(module (type (func)) (table 2 funcref) (func call 0 i32.const 0 call_indirect (type 0)))`,
		Hex:  "0061736d01000000010401600000030201000404017000020a0b010900100041001100000b",
	},
	{
		Name: "eh",
		WAT:  `(module (tag (param)) (func block try_table (catch 0 0) throw 0 end end))`,
		Hex:  "0061736d01000000010401600000030201000d030100000a10010e0002401f400100000008000b0b0b",
	},
	{
		Name: "elem_drop",
		WAT:  `(module (table 2 funcref) (elem func 0) (func elem.drop 0))`,
		Hex:  "0061736d0100000001040160000003020100040401700002090501010001000a07010500fc0d000b",
	},
	{
		Name: "gc",
		WAT:  `(module (func i32.const 1 ref.i31 i31.get_s drop ref.null any ref.test (ref eq) drop))`,
		Hex:  "0061736d01000000010401600000030201000a11010f004101fb1cfb1d1ad06efb146d1a0b",
	},
	{
		Name: "memory32",
		WAT:  `(module (memory 1 2) (func i32.const 0 i32.load offset=128 drop i32.const 0 memory.grow drop memory.size drop))`,
		Hex:  "0061736d01000000010401600000030201000504010101020a130111004100280280011a410040001a3f001a0b",
	},
	{
		Name: "mixed_memory",
		WAT:  `(module (memory 1 2) (memory i64 1 2) (func i64.const 0 i64.load 1 offset=4294967296 drop i32.const 0 i32.load 0 offset=128 drop i32.const 0 memory.grow 0 drop i64.const 0 memory.grow 1 drop))`,
		Hex:  "0061736d01000000010401600000030201000507020101020501020a20011e00420029430180808080101a4100280280011a410040001a420040011a0b",
	},
	{
		Name: "references",
		WAT:  `(module (table 1 funcref) (elem declare func 0) (func ref.null func i32.const 0 table.grow 0 drop i32.const 0 table.get 0 drop i32.const 0 ref.func 0 table.set 0 table.size 0 drop ref.null func ref.null func i32.const 0 select (result funcref) drop))`,
		Hex:  "0061736d0100000001040160000003020100040401700001090501030001000a25012300d0704100fc0f001a410025001a4100d2002600fc10001ad070d07041001c01701a0b",
	},
	{
		Name: "saturation",
		WAT:  `(module (func f32.const 1 i32.trunc_sat_f32_s i32.extend8_s drop))`,
		Hex:  "0061736d01000000010401600000030201000a0d010b00430000803ffc00c01a0b",
	},
	{
		Name: "scalar",
		WAT:  `(module (func (local i32) i32.const -2147483648 local.set 0 local.get 0 drop i64.const -9223372036854775808 drop f32.const 1.25 drop f64.const -2.5 drop))`,
		Hex:  "0061736d01000000010401600000030201000a2d012b01017f418080808078210020001a428080808080808080807f1a430000a03f1a4400000000000004c01a0b",
	},
	{
		Name: "simd",
		WAT:  `(module (memory 1) (func v128.const i32x4 1 2 3 4 i32x4.extract_lane 2 drop i32.const 0 v128.load offset=16 drop v128.const i32x4 1 2 3 4 v128.const i32x4 4 3 2 1 i8x16.shuffle 0 1 2 3 4 5 6 7 16 17 18 19 20 21 22 23 drop))`,
		Hex:  "0061736d010000000104016000000302010005030100010a58015600fd0c01000000020000000300000004000000fd1b021a4100fd0004101afd0c01000000020000000300000004000000fd0c04000000030000000200000001000000fd0d000102030405060710111213141516171a0b",
	},
	{
		Name: "table_copy",
		WAT:  `(module (table 2 funcref) (elem func 0) (func i32.const 0 i32.const 0 i32.const 1 table.copy))`,
		Hex:  "0061736d0100000001040160000003020100040401700002090501010001000a0e010c00410041004101fc0e00000b",
	},
	{
		Name: "table_init",
		WAT:  `(module (table 2 funcref) (elem func 0) (func i32.const 0 i32.const 0 i32.const 1 table.init 0))`,
		Hex:  "0061736d0100000001040160000003020100040401700002090501010001000a0e010c00410041004101fc0c00000b",
	},
	{
		Name: "tail",
		WAT:  `(module (func return_call 0))`,
		Hex:  "0061736d01000000010401600000030201000a0601040012000b",
	},
}

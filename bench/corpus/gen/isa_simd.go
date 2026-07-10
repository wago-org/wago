package main

import (
	"fmt"
	"strings"
)

// simdModules covers the deterministic core SIMD operations shared by the
// amd64 and arm64 railshot backends. Each vector operation is repeated in a
// coupled two-accumulator chain and reduced to i32 so both public engine APIs
// benchmark the same scalar result ABI.
func simdModules() []isaModule {
	return []isaModule{
		simdVectorModule("isa_simd_v128", 256, []simdDesc{
			unary("not", "v128.not"), binary("and", "v128.and"), binary("andnot", "v128.andnot"),
			binary("or", "v128.or"), binary("xor", "v128.xor"), ternary("bitselect", "v128.bitselect"),
		}),
		simdVectorModule("isa_simd_i8x16", 256, []simdDesc{
			unary("abs", "i8x16.abs"), unary("neg", "i8x16.neg"), unary("popcnt", "i8x16.popcnt"),
			binary("narrow_i16x8_s", "i8x16.narrow_i16x8_s"), binary("narrow_i16x8_u", "i8x16.narrow_i16x8_u"),
			binary("swizzle", "i8x16.swizzle"), shift("shl", "i8x16.shl"), shift("shr_s", "i8x16.shr_s"), shift("shr_u", "i8x16.shr_u"),
			binary("add", "i8x16.add"), binary("add_sat_s", "i8x16.add_sat_s"), binary("add_sat_u", "i8x16.add_sat_u"),
			binary("sub", "i8x16.sub"), binary("sub_sat_s", "i8x16.sub_sat_s"), binary("sub_sat_u", "i8x16.sub_sat_u"),
			binary("min_s", "i8x16.min_s"), binary("min_u", "i8x16.min_u"), binary("max_s", "i8x16.max_s"), binary("max_u", "i8x16.max_u"), binary("avgr_u", "i8x16.avgr_u"),
			binary("eq", "i8x16.eq"), binary("ne", "i8x16.ne"), binary("lt_s", "i8x16.lt_s"), binary("lt_u", "i8x16.lt_u"),
			binary("gt_s", "i8x16.gt_s"), binary("gt_u", "i8x16.gt_u"), binary("le_s", "i8x16.le_s"), binary("le_u", "i8x16.le_u"), binary("ge_s", "i8x16.ge_s"), binary("ge_u", "i8x16.ge_u"),
		}),
		simdVectorModule("isa_simd_i16x8", 256, []simdDesc{
			unary("abs", "i16x8.abs"), unary("neg", "i16x8.neg"), unary("extadd_pairwise_s", "i16x8.extadd_pairwise_i8x16_s"), unary("extadd_pairwise_u", "i16x8.extadd_pairwise_i8x16_u"),
			binary("narrow_i32x4_s", "i16x8.narrow_i32x4_s"), binary("narrow_i32x4_u", "i16x8.narrow_i32x4_u"),
			unary("extend_low_s", "i16x8.extend_low_i8x16_s"), unary("extend_high_s", "i16x8.extend_high_i8x16_s"), unary("extend_low_u", "i16x8.extend_low_i8x16_u"), unary("extend_high_u", "i16x8.extend_high_i8x16_u"),
			shift("shl", "i16x8.shl"), shift("shr_s", "i16x8.shr_s"), shift("shr_u", "i16x8.shr_u"), binary("add", "i16x8.add"), binary("sub", "i16x8.sub"), binary("mul", "i16x8.mul"),
			binary("add_sat_s", "i16x8.add_sat_s"), binary("add_sat_u", "i16x8.add_sat_u"), binary("sub_sat_s", "i16x8.sub_sat_s"), binary("sub_sat_u", "i16x8.sub_sat_u"),
			binary("min_s", "i16x8.min_s"), binary("min_u", "i16x8.min_u"), binary("max_s", "i16x8.max_s"), binary("max_u", "i16x8.max_u"), binary("avgr_u", "i16x8.avgr_u"), binary("q15mulr_sat_s", "i16x8.q15mulr_sat_s"),
			binary("extmul_low_s", "i16x8.extmul_low_i8x16_s"), binary("extmul_high_s", "i16x8.extmul_high_i8x16_s"), binary("extmul_low_u", "i16x8.extmul_low_i8x16_u"), binary("extmul_high_u", "i16x8.extmul_high_i8x16_u"),
			binary("eq", "i16x8.eq"), binary("ne", "i16x8.ne"), binary("lt_s", "i16x8.lt_s"), binary("lt_u", "i16x8.lt_u"), binary("gt_s", "i16x8.gt_s"), binary("gt_u", "i16x8.gt_u"), binary("le_s", "i16x8.le_s"), binary("le_u", "i16x8.le_u"), binary("ge_s", "i16x8.ge_s"), binary("ge_u", "i16x8.ge_u"),
		}),
		simdVectorModule("isa_simd_i32x4", 256, []simdDesc{
			unary("abs", "i32x4.abs"), unary("neg", "i32x4.neg"), unary("extadd_pairwise_s", "i32x4.extadd_pairwise_i16x8_s"), unary("extadd_pairwise_u", "i32x4.extadd_pairwise_i16x8_u"),
			unary("trunc_sat_f32x4_s", "i32x4.trunc_sat_f32x4_s"), unary("trunc_sat_f32x4_u", "i32x4.trunc_sat_f32x4_u"), unary("trunc_sat_f64x2_s_zero", "i32x4.trunc_sat_f64x2_s_zero"), unary("trunc_sat_f64x2_u_zero", "i32x4.trunc_sat_f64x2_u_zero"),
			unary("extend_low_s", "i32x4.extend_low_i16x8_s"), unary("extend_high_s", "i32x4.extend_high_i16x8_s"), unary("extend_low_u", "i32x4.extend_low_i16x8_u"), unary("extend_high_u", "i32x4.extend_high_i16x8_u"),
			shift("shl", "i32x4.shl"), shift("shr_s", "i32x4.shr_s"), shift("shr_u", "i32x4.shr_u"), binary("add", "i32x4.add"), binary("sub", "i32x4.sub"), binary("mul", "i32x4.mul"), binary("min_s", "i32x4.min_s"), binary("min_u", "i32x4.min_u"), binary("max_s", "i32x4.max_s"), binary("max_u", "i32x4.max_u"), binary("dot", "i32x4.dot_i16x8_s"),
			binary("extmul_low_s", "i32x4.extmul_low_i16x8_s"), binary("extmul_high_s", "i32x4.extmul_high_i16x8_s"), binary("extmul_low_u", "i32x4.extmul_low_i16x8_u"), binary("extmul_high_u", "i32x4.extmul_high_i16x8_u"),
			binary("eq", "i32x4.eq"), binary("ne", "i32x4.ne"), binary("lt_s", "i32x4.lt_s"), binary("lt_u", "i32x4.lt_u"), binary("gt_s", "i32x4.gt_s"), binary("gt_u", "i32x4.gt_u"), binary("le_s", "i32x4.le_s"), binary("le_u", "i32x4.le_u"), binary("ge_s", "i32x4.ge_s"), binary("ge_u", "i32x4.ge_u"),
		}),
		simdVectorModule("isa_simd_i64x2", 256, []simdDesc{
			unary("abs", "i64x2.abs"), unary("neg", "i64x2.neg"), unary("extend_low_s", "i64x2.extend_low_i32x4_s"), unary("extend_high_s", "i64x2.extend_high_i32x4_s"), unary("extend_low_u", "i64x2.extend_low_i32x4_u"), unary("extend_high_u", "i64x2.extend_high_i32x4_u"),
			shift("shl", "i64x2.shl"), shift("shr_s", "i64x2.shr_s"), shift("shr_u", "i64x2.shr_u"), binary("add", "i64x2.add"), binary("sub", "i64x2.sub"), binary("mul", "i64x2.mul"),
			binary("extmul_low_s", "i64x2.extmul_low_i32x4_s"), binary("extmul_high_s", "i64x2.extmul_high_i32x4_s"), binary("extmul_low_u", "i64x2.extmul_low_i32x4_u"), binary("extmul_high_u", "i64x2.extmul_high_i32x4_u"),
			binary("eq", "i64x2.eq"), binary("ne", "i64x2.ne"), binary("lt_s", "i64x2.lt_s"), binary("gt_s", "i64x2.gt_s"), binary("le_s", "i64x2.le_s"), binary("ge_s", "i64x2.ge_s"),
		}),
		simdFloatModule("isa_simd_f32x4", "f32x4", 256),
		simdFloatModule("isa_simd_f64x2", "f64x2", 256),
		simdReductionModule(256),
	}
}

type simdDesc struct{ name, expr string }

func unary(name, op string) simdDesc { return simdDesc{name, fmt.Sprintf("(%s (local.get $a))", op)} }
func binary(name, op string) simdDesc {
	return simdDesc{name, fmt.Sprintf("(%s (local.get $a) (local.get $b))", op)}
}
func ternary(name, op string) simdDesc {
	return simdDesc{name, fmt.Sprintf("(%s (local.get $a) (local.get $b) (v128.const i32x4 1431655765 -1431655766 1431655765 -1431655766))", op)}
}
func shift(name, op string) simdDesc {
	return simdDesc{name, fmt.Sprintf("(%s (local.get $a) (i32.const 3))", op)}
}

func simdVectorModule(file string, arg int, descs []simdDesc) isaModule {
	var b strings.Builder
	fmt.Fprintf(&b, ";; GENERATED by corpus/gen — do not edit by hand.\n;; %s: shared amd64/arm64 core SIMD operations.\n(module\n", file)
	exports := make([]string, 0, len(descs))
	for _, d := range descs {
		fmt.Fprintf(&b, "  (func (export %q) (param $n i32) (result i32)\n", d.name)
		b.WriteString("    (local $a v128) (local $b v128)\n")
		b.WriteString("    (local.set $a (v128.const i32x4 305419896 -19088744 324508639 610839776))\n")
		b.WriteString("    (local.set $b (v128.const i32x4 253635900 -1430532899 270544960 -559038737))\n")
		b.WriteString("    (block $done (loop $loop\n      (br_if $done (i32.eqz (local.get $n)))\n")
		for i := 0; i < isaUnroll; i++ {
			fmt.Fprintf(&b, "      (local.set $a %s)\n", d.expr)
			// Swap the accumulator names in the expression for the coupled step.
			exprB := strings.ReplaceAll(d.expr, "$a", "$tmp")
			exprB = strings.ReplaceAll(exprB, "$b", "$a")
			exprB = strings.ReplaceAll(exprB, "$tmp", "$b")
			fmt.Fprintf(&b, "      (local.set $b %s)\n", exprB)
		}
		b.WriteString("      (local.set $n (i32.sub (local.get $n) (i32.const 1)))\n      (br $loop)))\n")
		b.WriteString("    (i32x4.extract_lane 0 (v128.xor (local.get $a) (local.get $b))))\n")
		exports = append(exports, d.name)
	}
	b.WriteString(")\n")
	return isaModule{file: file + ".wasm", wat: b.String(), exports: exports, arg: arg}
}

func simdFloatModule(file, ty string, arg int) isaModule {
	descs := []simdDesc{
		unary("abs", ty+".abs"), unary("neg", ty+".neg"), unary("sqrt", ty+".sqrt"), unary("ceil", ty+".ceil"), unary("floor", ty+".floor"), unary("trunc", ty+".trunc"), unary("nearest", ty+".nearest"),
		binary("add", ty+".add"), binary("sub", ty+".sub"), binary("mul", ty+".mul"), binary("div", ty+".div"), binary("min", ty+".min"), binary("max", ty+".max"), binary("pmin", ty+".pmin"), binary("pmax", ty+".pmax"),
		binary("eq", ty+".eq"), binary("ne", ty+".ne"), binary("lt", ty+".lt"), binary("gt", ty+".gt"), binary("le", ty+".le"), binary("ge", ty+".ge"),
	}
	if ty == "f32x4" {
		descs = append(descs, unary("convert_i32x4_s", "f32x4.convert_i32x4_s"), unary("convert_i32x4_u", "f32x4.convert_i32x4_u"), unary("demote_f64x2_zero", "f32x4.demote_f64x2_zero"))
	} else {
		descs = append(descs, unary("convert_low_i32x4_s", "f64x2.convert_low_i32x4_s"), unary("convert_low_i32x4_u", "f64x2.convert_low_i32x4_u"), unary("promote_low_f32x4", "f64x2.promote_low_f32x4"))
	}
	return simdVectorModule(file, arg, descs)
}

func simdReductionModule(arg int) isaModule {
	var b strings.Builder
	b.WriteString(";; GENERATED by corpus/gen — do not edit by hand.\n;; isa_simd_reduce: scalar SIMD reductions.\n(module\n")
	ops := []string{"v128.any_true", "i8x16.all_true", "i8x16.bitmask", "i16x8.all_true", "i16x8.bitmask", "i32x4.all_true", "i32x4.bitmask", "i64x2.all_true", "i64x2.bitmask"}
	for _, op := range ops {
		name := strings.ReplaceAll(op, ".", "_")
		fmt.Fprintf(&b, "  (func (export %q) (param $n i32) (result i32)\n    (local $acc i32)\n", name)
		b.WriteString("    (block $done (loop $loop\n      (br_if $done (i32.eqz (local.get $n)))\n")
		for i := 0; i < isaUnroll; i++ {
			fmt.Fprintf(&b, "      (local.set $acc (i32.add (local.get $acc) (%s (v128.const i32x4 305419896 -19088744 324508639 610839776))))\n", op)
		}
		b.WriteString("      (local.set $n (i32.sub (local.get $n) (i32.const 1)))\n      (br $loop)))\n    (local.get $acc))\n")
	}
	b.WriteString(")\n")
	exports := make([]string, len(ops))
	for i, op := range ops {
		exports[i] = strings.ReplaceAll(op, ".", "_")
	}
	return isaModule{file: "isa_simd_reduce.wasm", wat: b.String(), exports: exports, arg: arg}
}

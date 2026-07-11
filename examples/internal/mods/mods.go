// Package mods builds the tiny WebAssembly modules the examples run against.
//
// Real projects compile guests from Rust, AssemblyScript, TinyGo, or C; these
// examples assemble a handful of minimal modules in-process so each example is
// self-contained and needs no external toolchain. The wasm here is deliberately
// small — the examples are about the wago Go API, not wasm authoring.
package mods

import "github.com/wago-org/wago/testutil/wasmtest"

// common opcodes
const (
	opLocalGet  = 0x20
	opGlobalGet = 0x23
	opGlobalSet = 0x24
	opI32Const  = 0x41
	opI32Add    = 0x6a
	opCall      = 0x10
	opEnd       = 0x0b
)

// wasm value-type bytes
const (
	i32 = 0x7f
	i64 = 0x7e
)

// Add is a pure module exporting add(a, b i32) -> i32 = a + b. No imports.
func Add() []byte {
	body := []byte{opLocalGet, 0, opLocalGet, 1, opI32Add, opEnd}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(ftype(vt(i32, i32), vt(i32)))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

// Counter exports a mutable i32 global "count" (init 0) and inc() -> i32 that
// increments it and returns the new value. Fresh instances start at 0.
func Counter() []byte {
	glob := []byte{i32, 0x01, opI32Const, 0x00, opEnd} // i32, mutable, i32.const 0
	body := []byte{
		opGlobalGet, 0, opI32Const, 0x01, opI32Add, // count + 1
		opGlobalSet, 0, opGlobalGet, 0, opEnd, // store; return
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(ftype(nil, vt(i32)))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(glob)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("inc", 0, 0),
			wasmtest.ExportEntry("count", 3, 0), // export kind 3 = global
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

// SquareViaHost imports host.mul(a, b i32) -> i32 and exports square(x) =
// mul(x, x). A returning host import exercises the synchronous host path.
func SquareViaHost() []byte {
	// types: 0 = (i32,i32)->i32 (import), 1 = (i32)->i32 (square)
	imp := importEntry("host", "mul", 0)
	// body: local.get 0; local.get 0; call 0; end
	body := []byte{opLocalGet, 0, opLocalGet, 0, opCall, 0, opEnd}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			ftype(vt(i32, i32), vt(i32)),
			ftype(vt(i32), vt(i32)),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), // square uses type 1
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("square", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

// MemWriter imports env.write(ptr, len i32) -> i32, holds the bytes msg in an
// exported memory as active data at offset 0, and exports run() -> i32 =
// write(0, len(msg)). The host reads the message straight out of guest memory.
func MemWriter(msg string) []byte {
	n := len(msg)
	imp := importEntry("env", "write", 0) // type 0 = (i32,i32)->i32
	body := []byte{opI32Const, byte(0), opI32Const, byte(n), opCall, 0, opEnd}
	memType := append([]byte{0x00}, wasmtest.ULEB(1)...) // 1 page, no max
	data := append([]byte{0x00, opI32Const, 0x00, opEnd}, append(wasmtest.ULEB(uint32(n)), msg...)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			ftype(vt(i32, i32), vt(i32)),
			ftype(nil, vt(i32)),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec(memType)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		wasmtest.Section(11, wasmtest.Vec(data)),
	)
}

// ImportCaller builds a module importing one function (module.name) with the
// given wasm signature and exporting export():()->results whose body just calls
// the import (pushing no arguments). Handy for wiring plugin imports like
// wago_timer.now_unix_ms()->i64. params must be empty (the call pushes nothing).
func ImportCaller(module, name, export string, results []byte) []byte {
	imp := importEntry(module, name, 0)
	body := []byte{opCall, 0, opEnd}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			ftype(nil, results), // import type
			ftype(nil, results), // export type (same results)
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry(export, 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

// I64 is the wasm i64 result-type byte, for ImportCaller results (e.g. a timer).
const I64 = i64

// I32 is the wasm i32 result-type byte.
const I32 = i32

// LogCaller imports wago_log.write(level, ptr, len i32) -> i32 and exports
// run() -> i32 that logs msg at the given level from an exported memory.
func LogCaller(level int, msg string) []byte {
	n := len(msg)
	imp := importEntry("wago_log", "write", 0) // (i32,i32,i32)->i32
	body := []byte{
		opI32Const, byte(level), // level
		opI32Const, 0x00, // ptr
		opI32Const, byte(n), // len
		opCall, 0x00, opEnd,
	}
	memType := append([]byte{0x00}, wasmtest.ULEB(1)...)
	data := append([]byte{0x00, opI32Const, 0x00, opEnd}, append(wasmtest.ULEB(uint32(n)), msg...)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			ftype(vt(i32, i32, i32), vt(i32)),
			ftype(nil, vt(i32)),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec(memType)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		wasmtest.Section(11, wasmtest.Vec(data)),
	)
}

// MetricsCaller imports wago_metrics.counter_add(name_ptr, name_len i32, delta
// i64) -> i32 and exports run() -> i32 that adds delta to the named counter,
// reading the counter name from an exported memory.
func MetricsCaller(name string, delta int) []byte {
	n := len(name)
	imp := importEntry("wago_metrics", "counter_add", 0) // (i32,i32,i64)->i32
	body := []byte{
		opI32Const, 0x00, // name_ptr
		opI32Const, byte(n), // name_len
		0x42, byte(delta), // i64.const delta
		opCall, 0x00, opEnd,
	}
	memType := append([]byte{0x00}, wasmtest.ULEB(1)...)
	data := append([]byte{0x00, opI32Const, 0x00, opEnd}, append(wasmtest.ULEB(uint32(n)), name...)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			ftype(vt(i32, i32, i64), vt(i32)),
			ftype(nil, vt(i32)),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec(memType)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		wasmtest.Section(11, wasmtest.Vec(data)),
	)
}

// ---- helpers ------------------------------------------------------------

// vt builds a slice of wasm value-type bytes.
func vt(types ...byte) []byte { return append([]byte(nil), types...) }

// ftype builds a wasm function type: 0x60, param count + bytes, result count + bytes.
func ftype(params, results []byte) []byte {
	out := []byte{0x60}
	out = append(out, wasmtest.ULEB(uint32(len(params)))...)
	out = append(out, params...)
	out = append(out, wasmtest.ULEB(uint32(len(results)))...)
	out = append(out, results...)
	return out
}

// importEntry builds a function import entry (module.name -> type index).
func importEntry(module, name string, typeIdx uint32) []byte {
	e := append(wasmtest.Name(module), wasmtest.Name(name)...)
	e = append(e, 0x00) // func import
	return append(e, wasmtest.ULEB(typeIdx)...)
}

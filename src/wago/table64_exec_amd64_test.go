//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func boundedTable64Module(max uint64) []byte {
	table := []byte{0x70, 0x05}
	table = append(table, uleb64(2)...)
	table = append(table, uleb64(max)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("clear", 0, 1),
			wasmtest.ExportEntry("is_null", 0, 2),
			wasmtest.ExportEntry("grow", 0, 3),
			wasmtest.ExportEntry("fill", 0, 4),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd2, 0x00, 0x20, 0x01, 0xfc, 0x11, 0x00, 0x0b}),
		)),
	)
}

func table32GetSetGrowSizeFillModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x02, 0x04})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x20, 0x01, 0xfc, 0x11, 0x00, 0x0b}),
		)),
	)
}

func table64WithInit(min, max uint64, expr []byte) []byte {
	out := []byte{0x40, 0x00, 0x70, 0x05} // table initializer, funcref, i64 min+max limits
	out = append(out, uleb64(min)...)
	out = append(out, uleb64(max)...)
	out = append(out, expr...)
	return append(out, 0x0b)
}

func table64ActiveElemExpr(offset []byte, exprs ...[]byte) []byte {
	out := []byte{0x04} // active table 0, funcref expression payloads
	out = append(out, offset...)
	out = append(out, 0x0b)
	return append(out, tableTestExprVec(exprs...)...)
}

func table64InitializerAndElementModule(offset []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(table64WithInit(2, 4, tableTestRefFuncExpr(0)))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(table64ActiveElemExpr(offset, tableTestRefNullFuncExpr()))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x4d, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
		)),
	)
}

func table64CallIndirectModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x04, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec(table64ActiveElemExpr(
			[]byte{0x42, 0x00}, tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(1),
		))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x42, 0x58, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
}

func table32CallIndirectModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
}

func table64LifecycleModule(max *uint64) []byte {
	table := []byte{0x70, 0x04}
	table = append(table, uleb64(2)...)
	if max != nil {
		table[1] = 0x05
		table = append(table, uleb64(*max)...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("grow", 0, 1),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
		)),
	)
}

func table64ImportLifecycleModule(min uint64, max *uint64) []byte {
	limits := []byte{0x04}
	limits = append(limits, uleb64(min)...)
	if max != nil {
		limits[0] = 0x05
		limits = append(limits, uleb64(*max)...)
	}
	imported := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	imported = append(imported, byte(wasm.ExternTable), 0x70)
	imported = append(imported, limits...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imported)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("grow", 0, 1),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}),
		)),
	)
}

func table64CopyModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0x04, 0x04})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("is_null", 0, 1),
			wasmtest.ExportEntry("copy", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(table64ActiveElemExpr(
			[]byte{0x42, 0x00}, tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0),
		))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x00, 0x0b}),
		)),
	)
}

func tableCopyActiveFuncsAt(tableIdx uint32, addr64 bool, offset byte, funcs ...uint32) []byte {
	out := append([]byte{0x02}, wasmtest.ULEB(tableIdx)...)
	if addr64 {
		out = append(out, 0x42, offset)
	} else {
		out = append(out, 0x41, offset)
	}
	out = append(out, 0x0b, 0x00)
	return append(out, tableTestFuncIdxVec(funcs...)...)
}

func twoLocalTableCopyModule(table0Addr64, table1Addr64 bool) []byte {
	addrType := func(addr64 bool) wasm.ValType {
		if addr64 {
			return wasm.I64
		}
		return wasm.I32
	}
	copyType := func(dst64, src64 bool) []wasm.ValType {
		length := wasm.I32
		if dst64 && src64 {
			length = wasm.I64
		}
		return []wasm.ValType{addrType(dst64), addrType(src64), length}
	}
	tableType := func(addr64 bool) []byte {
		flags := byte(0x01)
		if addr64 {
			flags = 0x05
		}
		return []byte{0x70, flags, 0x04, 0x04}
	}
	copyBody := func(dst, src byte) []byte {
		return []byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, dst, src, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(copyType(table0Addr64, table0Addr64), nil),
			wasmtest.FuncType(copyType(table1Addr64, table0Addr64), nil),
			wasmtest.FuncType(copyType(table0Addr64, table1Addr64), nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(4, wasmtest.Vec(tableType(table0Addr64), tableType(table1Addr64))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copy00", 0, 1),
			wasmtest.ExportEntry("copy10", 0, 2),
			wasmtest.ExportEntry("copy01", 0, 3),
			wasmtest.ExportEntry("t0", 1, 0),
			wasmtest.ExportEntry("t1", 1, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableCopyActiveFuncsAt(0, table0Addr64, 0, 0),
			tableCopyActiveFuncsAt(0, table0Addr64, 3, 0),
			tableCopyActiveFuncsAt(1, table1Addr64, 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code(copyBody(0, 0)),
			wasmtest.Code(copyBody(1, 0)),
			wasmtest.Code(copyBody(0, 1)),
		)),
	)
}

func twoLocalTableReadWriteModule(table0Addr64, table1Addr64 bool) []byte {
	addrType := func(addr64 bool) wasm.ValType {
		if addr64 {
			return wasm.I64
		}
		return wasm.I32
	}
	tableType := func(addr64 bool) []byte {
		flags := byte(0x01)
		if addr64 {
			flags = 0x05
		}
		return []byte{0x70, flags, 0x02, 0x04}
	}
	sizeBody := func(table byte) []byte { return []byte{0xfc, 0x10, table, 0x0b} }
	isNullBody := func(table byte) []byte { return []byte{0x20, 0x00, 0x25, table, 0xd1, 0x0b} }
	setFromBody := func(dst, src byte) []byte {
		return []byte{0x20, 0x00, 0x20, 0x01, 0x25, src, 0x26, dst, 0x0b}
	}
	clearBody := func(table byte) []byte {
		return []byte{0x20, 0x00, 0xd0, 0x70, 0x26, table, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{addrType(table0Addr64)}),
			wasmtest.FuncType(nil, []wasm.ValType{addrType(table1Addr64)}),
			wasmtest.FuncType([]wasm.ValType{addrType(table0Addr64)}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType(table1Addr64)}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType(table0Addr64), addrType(table1Addr64)}, nil),
			wasmtest.FuncType([]wasm.ValType{addrType(table1Addr64), addrType(table0Addr64)}, nil),
			wasmtest.FuncType([]wasm.ValType{addrType(table0Addr64)}, nil),
			wasmtest.FuncType([]wasm.ValType{addrType(table1Addr64)}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4),
			wasmtest.ULEB(5), wasmtest.ULEB(6), wasmtest.ULEB(7), wasmtest.ULEB(8),
		)),
		wasmtest.Section(4, wasmtest.Vec(tableType(table0Addr64), tableType(table1Addr64))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size0", 0, 1),
			wasmtest.ExportEntry("size1", 0, 2),
			wasmtest.ExportEntry("is_null0", 0, 3),
			wasmtest.ExportEntry("is_null1", 0, 4),
			wasmtest.ExportEntry("set01", 0, 5),
			wasmtest.ExportEntry("set10", 0, 6),
			wasmtest.ExportEntry("clear0", 0, 7),
			wasmtest.ExportEntry("clear1", 0, 8),
			wasmtest.ExportEntry("t0", 1, 0),
			wasmtest.ExportEntry("t1", 1, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableCopyActiveFuncsAt(0, table0Addr64, 0, 0),
			tableCopyActiveFuncsAt(1, table1Addr64, 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code(sizeBody(0)),
			wasmtest.Code(sizeBody(1)),
			wasmtest.Code(isNullBody(0)),
			wasmtest.Code(isNullBody(1)),
			wasmtest.Code(setFromBody(0, 1)),
			wasmtest.Code(setFromBody(1, 0)),
			wasmtest.Code(clearBody(0)),
			wasmtest.Code(clearBody(1)),
		)),
	)
}

func twoLocalTableGrowFillModule(table0Addr64, table1Addr64 bool) []byte {
	addrType := func(addr64 bool) wasm.ValType {
		if addr64 {
			return wasm.I64
		}
		return wasm.I32
	}
	tableType := func(addr64 bool) []byte {
		flags := byte(0x01)
		if addr64 {
			flags = 0x05
		}
		return []byte{0x70, flags, 0x02, 0x04}
	}
	sizeBody := func(table byte) []byte { return []byte{0xfc, 0x10, table, 0x0b} }
	isNullBody := func(table byte) []byte { return []byte{0x20, 0x00, 0x25, table, 0xd1, 0x0b} }
	growBody := func(table byte, nonNull bool) []byte {
		body := []byte{0xd0, 0x70}
		if nonNull {
			body = []byte{0xd2, 0x00}
		}
		return append(body, 0x20, 0x00, 0xfc, 0x0f, table, 0x0b)
	}
	fillBody := func(table byte, nonNull bool) []byte {
		body := []byte{0x20, 0x00, 0xd0, 0x70}
		if nonNull {
			body = []byte{0x20, 0x00, 0xd2, 0x00}
		}
		return append(body, 0x20, 0x01, 0xfc, 0x11, table, 0x0b)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{addrType(table0Addr64)}),
			wasmtest.FuncType(nil, []wasm.ValType{addrType(table1Addr64)}),
			wasmtest.FuncType([]wasm.ValType{addrType(table0Addr64)}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType(table1Addr64)}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType(table0Addr64)}, []wasm.ValType{addrType(table0Addr64)}),
			wasmtest.FuncType([]wasm.ValType{addrType(table1Addr64)}, []wasm.ValType{addrType(table1Addr64)}),
			wasmtest.FuncType([]wasm.ValType{addrType(table0Addr64), addrType(table0Addr64)}, nil),
			wasmtest.FuncType([]wasm.ValType{addrType(table1Addr64), addrType(table1Addr64)}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4),
			wasmtest.ULEB(5), wasmtest.ULEB(6), wasmtest.ULEB(7), wasmtest.ULEB(8),
		)),
		wasmtest.Section(4, wasmtest.Vec(tableType(table0Addr64), tableType(table1Addr64))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size0", 0, 1),
			wasmtest.ExportEntry("size1", 0, 2),
			wasmtest.ExportEntry("is_null0", 0, 3),
			wasmtest.ExportEntry("is_null1", 0, 4),
			wasmtest.ExportEntry("grow0", 0, 5),
			wasmtest.ExportEntry("grow1", 0, 6),
			wasmtest.ExportEntry("fill0", 0, 7),
			wasmtest.ExportEntry("fill1", 0, 8),
			wasmtest.ExportEntry("t0", 1, 0),
			wasmtest.ExportEntry("t1", 1, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableCopyActiveFuncsAt(0, table0Addr64, 0, 0),
			tableCopyActiveFuncsAt(1, table1Addr64, 0, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code(sizeBody(0)),
			wasmtest.Code(sizeBody(1)),
			wasmtest.Code(isNullBody(0)),
			wasmtest.Code(isNullBody(1)),
			wasmtest.Code(growBody(0, true)),
			wasmtest.Code(growBody(1, false)),
			wasmtest.Code(fillBody(0, true)),
			wasmtest.Code(fillBody(1, true)),
		)),
	)
}

func twoLocalTableInitDropModule(table0Addr64, table1Addr64 bool) []byte {
	addrType := func(addr64 bool) wasm.ValType {
		if addr64 {
			return wasm.I64
		}
		return wasm.I32
	}
	tableType := func(addr64 bool) []byte {
		flags := byte(0x01)
		if addr64 {
			flags = 0x05
		}
		return []byte{0x70, flags, 0x04, 0x04}
	}
	isNullBody := func(table byte) []byte { return []byte{0x20, 0x00, 0x25, table, 0xd1, 0x0b} }
	initBody := func(elem, table byte) []byte {
		return []byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0c, elem, table, 0x0b}
	}
	dropBody := func(elem byte) []byte { return []byte{0xfc, 0x0d, elem, 0x0b} }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType(table0Addr64)}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType(table1Addr64)}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType(table0Addr64), wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{addrType(table1Addr64), wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4),
			wasmtest.ULEB(5), wasmtest.ULEB(5), wasmtest.ULEB(4), wasmtest.ULEB(5),
		)),
		wasmtest.Section(4, wasmtest.Vec(tableType(table0Addr64), tableType(table1Addr64))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("is_null0", 0, 1),
			wasmtest.ExportEntry("is_null1", 0, 2),
			wasmtest.ExportEntry("init0", 0, 3),
			wasmtest.ExportEntry("init1", 0, 4),
			wasmtest.ExportEntry("drop0", 0, 5),
			wasmtest.ExportEntry("drop1", 0, 6),
			wasmtest.ExportEntry("init_decl", 0, 7),
			wasmtest.ExportEntry("drop_decl", 0, 8),
			wasmtest.ExportEntry("t0", 1, 0),
			wasmtest.ExportEntry("t1", 1, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestPassiveElemExpr(tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0)),
			tableTestPassiveElemExpr(tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0), tableTestRefNullFuncExpr()),
			tableTestDeclarativeElem(0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code(isNullBody(0)),
			wasmtest.Code(isNullBody(1)),
			wasmtest.Code(initBody(0, 0)),
			wasmtest.Code(initBody(1, 1)),
			wasmtest.Code(dropBody(0)),
			wasmtest.Code(dropBody(1)),
			wasmtest.Code(initBody(2, 1)),
			wasmtest.Code(dropBody(2)),
		)),
	)
}

func tableInitDropModule(addr64 bool) []byte {
	addrType := wasm.I32
	limits := []byte{0x70, 0x01, 0x04, 0x04}
	if addr64 {
		addrType = wasm.I64
		limits[1] = 0x05
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{addrType, wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(4, wasmtest.Vec(limits)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("is_null", 0, 1),
			wasmtest.ExportEntry("init", 0, 2),
			wasmtest.ExportEntry("drop", 0, 3),
			wasmtest.ExportEntry("init_decl", 0, 4),
			wasmtest.ExportEntry("drop_decl", 0, 5),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestPassiveElemExpr(tableTestRefFuncExpr(0), tableTestRefNullFuncExpr(), tableTestRefFuncExpr(0)),
			tableTestDeclarativeElem(0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0c, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x0d, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0c, 0x01, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x0d, 0x01, 0x0b}),
		)),
	)
}

func table32CopyModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x04, 0x04})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x00, 0x0b}))),
	)
}

func compileStagedTable64(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedTable64LocalGetSetSizeAndProductRoundTrip(t *testing.T) {
	module := boundedTable64Module(4)
	if _, err := Compile(nil, module); err == nil || !strings.Contains(err.Error(), "table64") {
		t.Fatalf("public table64 compile error = %v", err)
	}
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile staged table64: %v", err)
	}
	defer compiled.Close()
	if !compiled.requiredFeatures.IsEnabled(CoreFeatureTable64) {
		t.Fatalf("table64 required features = %s", compiled.requiredFeatures)
	}
	if !compiled.HasTable || !compiled.TableAddr64 || compiled.TableSize != 2 || compiled.TableMax != 4 || !compiled.TableHasMax {
		t.Fatalf("compiled table64 shape = present %v addr64 %v size/max %d/%d hasMax %v", compiled.HasTable, compiled.TableAddr64, compiled.TableSize, compiled.TableMax, compiled.TableHasMax)
	}
	meta := (&Module{c: compiled}).Metadata()
	if len(meta.Tables) != 1 || !meta.Tables[0].Addr64 || meta.Tables[0].Min != 2 || meta.Tables[0].Max != 4 || !meta.Tables[0].HasMax || !reflect.DeepEqual(meta.Tables[0].Exports, []string{"table"}) {
		t.Fatalf("table64 metadata = %#v", meta.Tables)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64: %v", err)
	}
	if blob[4] != 26 {
		t.Fatalf("table64 codec version = %d, want 26", blob[4])
	}
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public table64 codec load error = %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("decode table64 metadata: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true // private execution proof; codec never serializes admission
	loaded.hasTableExportMetadata = true
	if !loaded.requiredFeatures.IsEnabled(CoreFeatureTable64) || !loaded.TableAddr64 || !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
		t.Fatalf("table64 codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
	}
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tables cannot be snapshotted") {
		t.Fatalf("table64 snapshot error = %v", err)
	}

	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate table64: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("size"); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("table64.size = %v, err=%v", got, err)
	}
	for _, index := range []uint64{0, 1} {
		if got, err := in.Invoke("is_null", index); err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("table64.get(%d) null = %v, err=%v", index, got, err)
		}
		if _, err := in.Invoke("clear", index); err != nil {
			t.Fatalf("table64.set(%d): %v", index, err)
		}
	}
	for _, index := range []uint64{2, 1 << 32, ^uint64(0)} {
		if _, err := in.Invoke("is_null", index); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
			t.Fatalf("table64.get(%d) error = %v", index, err)
		}
		if _, err := in.Invoke("clear", index); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
			t.Fatalf("table64.set(%d) error = %v", index, err)
		}
	}
	if got, err := in.Invoke("is_null", 1); err != nil || got[0] != 1 {
		t.Fatalf("table64 state changed after traps = %v, err=%v", got, err)
	}
	if _, err := in.Invoke("fill", 0, 2); err != nil {
		t.Fatalf("table64.fill full range: %v", err)
	}
	for _, index := range []uint64{0, 1} {
		if got, err := in.Invoke("is_null", index); err != nil || got[0] != 0 {
			t.Fatalf("table64.fill entry %d null = %v, err=%v", index, got, err)
		}
	}
	if _, err := in.Invoke("clear", 1); err != nil {
		t.Fatalf("clear table64 fill sentinel: %v", err)
	}
	if _, err := in.Invoke("fill", 2, 0); err != nil {
		t.Fatalf("zero-length table64.fill at boundary: %v", err)
	}
	for _, args := range [][2]uint64{{1, 2}, {1 << 32, 0}, {0, 1 << 32}, {^uint64(0), 2}} {
		if _, err := in.Invoke("fill", args[0], args[1]); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
			t.Fatalf("table64.fill(%d,%d) error = %v", args[0], args[1], err)
		}
		if got, err := in.Invoke("is_null", 1); err != nil || got[0] != 1 {
			t.Fatalf("trapping table64.fill changed sentinel = %v, err=%v", got, err)
		}
	}
	if got, err := in.Invoke("is_null", 0); err != nil || got[0] != 0 {
		t.Fatalf("trapping table64.fill changed prior entry = %v, err=%v", got, err)
	}
	if _, err := in.Invoke("clear", 0); err != nil {
		t.Fatalf("clear table64 fill entry: %v", err)
	}
	if got, err := in.Invoke("grow", 1); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("table64.grow(1) = %v, err=%v, want [2]", got, err)
	}
	if got, err := in.Invoke("size"); err != nil || got[0] != 3 {
		t.Fatalf("table64 size after grow = %v, err=%v", got, err)
	}
	if got, err := in.Invoke("is_null", 2); err != nil || got[0] != 1 {
		t.Fatalf("grown table64 entry = %v, err=%v", got, err)
	}
	if got, err := in.Invoke("grow", 0); err != nil || got[0] != 3 {
		t.Fatalf("table64.grow(0) = %v, err=%v", got, err)
	}
	if got, err := in.Invoke("grow", 1); err != nil || got[0] != 3 {
		t.Fatalf("table64 grow to maximum = %v, err=%v", got, err)
	}
	for _, delta := range []uint64{1, 1 << 32, ^uint64(0)} {
		if got, err := in.Invoke("grow", delta); err != nil || len(got) != 1 || got[0] != ^uint64(0) {
			t.Fatalf("table64.grow(%d) = %v, err=%v, want [-1]", delta, got, err)
		}
		if got, err := in.Invoke("size"); err != nil || got[0] != 4 {
			t.Fatalf("failed table64 grow changed size = %v, err=%v", got, err)
		}
	}

	loadedIn, err := instantiateCore(&loaded, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate codec-reloaded table64: %v", err)
	}
	defer loadedIn.Close()
	if _, err := loadedIn.Invoke("fill", 0, 2); err != nil {
		t.Fatalf("codec-reloaded table64.fill: %v", err)
	}
	if got, err := loadedIn.Invoke("is_null", 1); err != nil || got[0] != 0 {
		t.Fatalf("codec-reloaded table64.fill entry = %v, err=%v", got, err)
	}
	if got, err := loadedIn.Invoke("grow", 2); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("codec-reloaded table64.grow = %v, err=%v", got, err)
	}
	if got, err := loadedIn.Invoke("grow", 1<<32); err != nil || len(got) != 1 || got[0] != ^uint64(0) {
		t.Fatalf("codec-reloaded high-delta table64.grow = %v, err=%v", got, err)
	}
}

func TestStagedTable64InitializerAndI64ActiveElementRoundTrip(t *testing.T) {
	module := table64InitializerAndElementModule([]byte{0x42, 0x01})
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile table64 initializer/element: %v", err)
	}
	defer compiled.Close()
	if !compiled.HasTableInitFunc || compiled.TableInitFunc != 0 || len(compiled.Elems) != 1 || len(compiled.Elems[0].Offset.Expr) == 0 {
		t.Fatalf("table64 initializer/element metadata = init %v/%d elems %#v", compiled.HasTableInitFunc, compiled.TableInitFunc, compiled.Elems)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64 initializer/element: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64 initializer/element: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true

	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s table64 initializer/element: %v", name, err)
		}
		if got, err := in.Invoke("is_null", 0); err != nil || len(got) != 1 || got[0] != 0 {
			_ = in.Close()
			t.Fatalf("%s table initializer entry = %v, err=%v", name, got, err)
		}
		if got, err := in.Invoke("is_null", 1); err != nil || len(got) != 1 || got[0] != 1 {
			_ = in.Close()
			t.Fatalf("%s active element override = %v, err=%v", name, got, err)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s table64 instance: %v", name, err)
		}
	}

	oob, err := compileStagedTable64(table64InitializerAndElementModule([]byte{0x42, 0x7f}))
	if err != nil {
		t.Fatalf("compile high-offset table64 element: %v", err)
	}
	defer oob.Close()
	if in, err := instantiateCore(oob, InstantiateOptions{}); err == nil || in != nil || !strings.Contains(err.Error(), "18446744073709551615") {
		t.Fatalf("high-offset table64 element instantiate = %v, %v", in, err)
	}
}

func TestStagedTable64CallIndirectFullWidthAndCodecRoundTrip(t *testing.T) {
	module := table64CallIndirectModule()
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile table64 call_indirect: %v", err)
	}
	defer compiled.Close()
	if !compiled.TableAddr64 || compiled.TableSize != 3 || compiled.TableMax != 3 || compiled.TableHasMax || len(compiled.Elems) != 1 {
		t.Fatalf("table64 call_indirect metadata = addr64 %v size/max %d/%d hasMax %v elems %d", compiled.TableAddr64, compiled.TableSize, compiled.TableMax, compiled.TableHasMax, len(compiled.Elems))
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64 call_indirect: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64 call_indirect: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true
	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s table64 call_indirect: %v", name, err)
		}
		if got, err := in.Invoke("call", 0); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			in.Close()
			t.Fatalf("%s table64 call_indirect result = %v, err=%v", name, got, err)
		}
		for _, index := range []uint64{1, 3, 1 << 32, ^uint64(0)} {
			if _, err := in.Invoke("call", index); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
				in.Close()
				t.Fatalf("%s table64 call_indirect(%d) = %v, want null/bounds trap", name, index, err)
			}
		}
		if _, err := in.Invoke("call", 2); err == nil || !strings.Contains(err.Error(), "signature") {
			in.Close()
			t.Fatalf("%s table64 call_indirect wrong signature = %v", name, err)
		}
		if got, err := in.Invoke("call", 0); err != nil || AsI32(got[0]) != 42 {
			in.Close()
			t.Fatalf("%s table64 call_indirect after traps = %v, err=%v", name, got, err)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s table64 call_indirect: %v", name, err)
		}
	}
}

func TestStagedTable64CopyFullWidthOverlapAtomicityAndCodecRoundTrip(t *testing.T) {
	module := table64CopyModule()
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile table64.copy: %v", err)
	}
	defer compiled.Close()
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64.copy: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64.copy: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true

	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s table64.copy: %v", name, err)
		}
		state := func() [4]uint64 {
			var out [4]uint64
			for i := range out {
				got, err := in.Invoke("is_null", uint64(i))
				if err != nil || len(got) != 1 {
					in.Close()
					t.Fatalf("%s table64.copy state[%d] = %v, err=%v", name, i, got, err)
				}
				out[i] = got[0]
			}
			return out
		}
		if got := state(); got != [4]uint64{0, 1, 1, 0} {
			in.Close()
			t.Fatalf("%s initial table64.copy state = %v", name, got)
		}
		if _, err := in.Invoke("copy", 1, 0, 3); err != nil {
			in.Close()
			t.Fatalf("%s overlapping forward table64.copy: %v", name, err)
		}
		if got := state(); got != [4]uint64{0, 0, 1, 1} {
			in.Close()
			t.Fatalf("%s forward table64.copy state = %v", name, got)
		}
		if _, err := in.Invoke("copy", 0, 1, 3); err != nil {
			in.Close()
			t.Fatalf("%s overlapping backward table64.copy: %v", name, err)
		}
		if got := state(); got != [4]uint64{0, 1, 1, 1} {
			in.Close()
			t.Fatalf("%s backward table64.copy state = %v", name, got)
		}
		if _, err := in.Invoke("copy", 4, 4, 0); err != nil {
			in.Close()
			t.Fatalf("%s zero-length boundary table64.copy: %v", name, err)
		}
		before := state()
		for _, args := range [][3]uint64{
			{^uint64(0), 0, 2}, {0, ^uint64(0), 2}, {0, 0, 5},
			{1 << 32, 0, 0}, {0, 1 << 32, 0}, {0, 0, 1 << 32},
		} {
			if _, err := in.Invoke("copy", args[0], args[1], args[2]); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
				in.Close()
				t.Fatalf("%s table64.copy(%d,%d,%d) = %v", name, args[0], args[1], args[2], err)
			}
			if got := state(); got != before {
				in.Close()
				t.Fatalf("%s trapping table64.copy changed state: got %v want %v", name, got, before)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s table64.copy: %v", name, err)
		}
	}

	ordinary, err := Compile(nil, table32CopyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	staged, err := compileWithFrontendFeatures(cfg, table32CopyModule(), features)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(ordinary.Code, staged.Code) {
		t.Fatal("enabling staged table64.copy changed table32 code bytes")
	}
}

func TestStagedTable64PassiveInitDropFullWidthAtomicityAndCodecRoundTrip(t *testing.T) {
	module := tableInitDropModule(true)
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile table64.init/drop: %v", err)
	}
	defer compiled.Close()
	if len(compiled.passiveElems) != 2 || len(compiled.passiveElems[0].Values) != 3 || len(compiled.passiveElems[1].Values) != 0 {
		t.Fatalf("table64 passive/declarative metadata = %#v", compiled.passiveElems)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64.init/drop: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64.init/drop: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true
	if len(loaded.passiveElems) != 2 || len(loaded.passiveElems[0].Values) != 3 || len(loaded.passiveElems[1].Values) != 0 {
		t.Fatalf("reloaded table64 passive/declarative metadata = %#v", loaded.passiveElems)
	}

	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s table64.init/drop: %v", name, err)
		}
		state := func() [4]uint64 {
			var out [4]uint64
			for i := range out {
				got, err := in.Invoke("is_null", uint64(i))
				if err != nil || len(got) != 1 {
					in.Close()
					t.Fatalf("%s table64.init state[%d] = %v, err=%v", name, i, got, err)
				}
				out[i] = got[0]
			}
			return out
		}
		if got := state(); got != [4]uint64{1, 1, 1, 1} {
			in.Close()
			t.Fatalf("%s initial table64.init state = %v", name, got)
		}
		if _, err := in.Invoke("init", 1, 0, 3); err != nil {
			in.Close()
			t.Fatalf("%s table64.init: %v", name, err)
		}
		if got := state(); got != [4]uint64{1, 0, 1, 0} {
			in.Close()
			t.Fatalf("%s initialized table64 state = %v", name, got)
		}
		if _, err := in.Invoke("init", 4, 3, 0); err != nil {
			in.Close()
			t.Fatalf("%s zero-length table64.init boundary: %v", name, err)
		}
		before := state()
		for _, args := range [][3]uint64{
			{^uint64(0), 0, 2}, {3, 0, 2}, {0, 2, 2}, {0, uint64(^uint32(0)), 2}, {1 << 32, 0, 0},
		} {
			if _, err := in.Invoke("init", args[0], args[1], args[2]); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				in.Close()
				t.Fatalf("%s table64.init(%d,%d,%d) = %v", name, args[0], args[1], args[2], err)
			}
			if got := state(); got != before {
				in.Close()
				t.Fatalf("%s trapping table64.init changed state: got %v want %v", name, got, before)
			}
		}
		if _, err := in.Invoke("init_decl", 4, 0, 0); err != nil {
			in.Close()
			t.Fatalf("%s zero-length declarative table64.init: %v", name, err)
		}
		if _, err := in.Invoke("init_decl", 0, 0, 1); err == nil {
			in.Close()
			t.Fatalf("%s nonzero declarative table64.init succeeded", name)
		}
		if _, err := in.Invoke("drop_decl"); err != nil {
			in.Close()
			t.Fatalf("%s declarative elem.drop: %v", name, err)
		}
		if _, err := in.Invoke("drop"); err != nil {
			in.Close()
			t.Fatalf("%s table64 elem.drop: %v", name, err)
		}
		if _, err := in.Invoke("drop"); err != nil {
			in.Close()
			t.Fatalf("%s repeated table64 elem.drop: %v", name, err)
		}
		if _, err := in.Invoke("init", 4, 0, 0); err != nil {
			in.Close()
			t.Fatalf("%s zero-length table64.init after drop: %v", name, err)
		}
		if _, err := in.Invoke("init", 0, 0, 1); err == nil {
			in.Close()
			t.Fatalf("%s nonzero table64.init after drop succeeded", name)
		}
		if got := state(); got != before {
			in.Close()
			t.Fatalf("%s dropped/trapping table64.init changed state: got %v want %v", name, got, before)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s table64.init/drop: %v", name, err)
		}
	}

	ordinary, err := Compile(nil, tableInitDropModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	staged, err := compileWithFrontendFeatures(cfg, tableInitDropModule(false), features)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(ordinary.Code, staged.Code) {
		t.Fatal("enabling staged table64.init/drop changed table32 code bytes")
	}
}

func TestStagedTable64TwoLocalCopyMixedWidthsDirectoryAndCodecRoundTrip(t *testing.T) {
	cases := []struct {
		name         string
		table0Addr64 bool
		table1Addr64 bool
	}{
		{name: "table64-table64", table0Addr64: true, table1Addr64: true},
		{name: "table64-table32", table0Addr64: true, table1Addr64: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			module := twoLocalTableCopyModule(tc.table0Addr64, tc.table1Addr64)
			compiled, err := compileStagedTable64(module)
			if err != nil {
				t.Fatalf("compile two-local table.copy: %v", err)
			}
			defer compiled.Close()
			meta := (&Module{c: compiled}).Metadata()
			if len(meta.Tables) != 2 || meta.Tables[0].Addr64 != tc.table0Addr64 || meta.Tables[1].Addr64 != tc.table1Addr64 ||
				meta.Tables[0].Min != 4 || meta.Tables[0].Max != 4 || meta.Tables[1].Min != 4 || meta.Tables[1].Max != 4 ||
				!reflect.DeepEqual(meta.Tables[0].Exports, []string{"t0"}) || !reflect.DeepEqual(meta.Tables[1].Exports, []string{"t1"}) {
				t.Fatalf("two-local table.copy metadata = %#v", meta.Tables)
			}
			blob, err := compiled.MarshalBinary()
			if err != nil {
				t.Fatalf("marshal two-local table.copy: %v", err)
			}
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatalf("reload two-local table.copy: %v", err)
			}
			defer loaded.Close()
			loaded.stagedTable64 = true
			loaded.hasTableExportMetadata = true
			if !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
				t.Fatalf("two-local table.copy codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
			}

			for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
				in, err := instantiateCore(c, InstantiateOptions{})
				if err != nil {
					t.Fatalf("instantiate %s two-local table.copy: %v", name, err)
				}
				state := func(table int) [4]uint64 {
					desc := in.tableDescriptor(table)
					if len(desc) < 8+4*runtime.TableEntryBytes {
						in.Close()
						t.Fatalf("%s table %d descriptor length = %d", name, table, len(desc))
					}
					var out [4]uint64
					for i := range out {
						if binary.LittleEndian.Uint64(desc[8+i*runtime.TableEntryBytes:]) != 0 {
							out[i] = 1
						}
					}
					return out
				}
				if got := state(0); got != [4]uint64{1, 0, 0, 1} {
					in.Close()
					t.Fatalf("%s initial table 0 = %v", name, got)
				}
				if got := state(1); got != [4]uint64{0, 1, 0, 0} {
					in.Close()
					t.Fatalf("%s initial table 1 = %v", name, got)
				}
				if _, err := in.Invoke("copy10", 0, 0, 3); err != nil {
					in.Close()
					t.Fatalf("%s cross-table copy10: %v", name, err)
				}
				if got := state(1); got != [4]uint64{1, 0, 0, 0} {
					in.Close()
					t.Fatalf("%s cross-table copy10 state = %v", name, got)
				}
				if _, err := in.Invoke("copy00", 1, 0, 3); err != nil {
					in.Close()
					t.Fatalf("%s overlapping table.copy: %v", name, err)
				}
				if got := state(0); got != [4]uint64{1, 1, 0, 0} {
					in.Close()
					t.Fatalf("%s overlapping table.copy state = %v", name, got)
				}
				if _, err := in.Invoke("copy01", 0, 0, 4); err != nil {
					in.Close()
					t.Fatalf("%s reverse cross-table copy01: %v", name, err)
				}
				if got := state(0); got != [4]uint64{1, 0, 0, 0} {
					in.Close()
					t.Fatalf("%s reverse cross-table copy01 state = %v", name, got)
				}
				if _, err := in.Invoke("copy10", 4, 4, 0); err != nil {
					in.Close()
					t.Fatalf("%s zero-length cross-table boundary: %v", name, err)
				}
				if !tc.table1Addr64 {
					if _, err := in.Invoke("copy10", 4, 4, uint64(1)<<32); err != nil {
						in.Close()
						t.Fatalf("%s mixed minimum-width zero length: %v", name, err)
					}
					if _, err := in.Invoke("copy01", 2, uint64(1)<<32, 1); err != nil {
						in.Close()
						t.Fatalf("%s mixed table32 source canonicalization: %v", name, err)
					}
					if got := state(0); got != [4]uint64{1, 0, 1, 0} {
						in.Close()
						t.Fatalf("%s mixed-width copy state = %v", name, got)
					}
				}
				before0, before1 := state(0), state(1)
				trapArgs := [][3]uint64{{^uint64(0), 0, 2}, {0, ^uint64(0), 2}, {0, 0, 5}}
				if tc.table1Addr64 {
					trapArgs = append(trapArgs, [3]uint64{0, 0, uint64(1) << 32})
				} else {
					trapArgs = append(trapArgs, [3]uint64{uint64(^uint32(0)), 0, 2})
				}
				for _, args := range trapArgs {
					if _, err := in.Invoke("copy10", args[0], args[1], args[2]); err == nil || !strings.Contains(err.Error(), "out of bounds") {
						in.Close()
						t.Fatalf("%s copy10(%d,%d,%d) = %v", name, args[0], args[1], args[2], err)
					}
					if got0, got1 := state(0), state(1); got0 != before0 || got1 != before1 {
						in.Close()
						t.Fatalf("%s trapping cross-table copy changed state: got %v/%v want %v/%v", name, got0, got1, before0, before1)
					}
				}
				if _, err := in.Invoke("copy01", ^uint64(0), 0, 2); err == nil || !strings.Contains(err.Error(), "out of bounds") {
					in.Close()
					t.Fatalf("%s copy01 destination carry = %v", name, err)
				}
				if got0, got1 := state(0), state(1); got0 != before0 || got1 != before1 {
					in.Close()
					t.Fatalf("%s trapping reverse copy changed state: got %v/%v want %v/%v", name, got0, got1, before0, before1)
				}
				if err := in.Close(); err != nil {
					t.Fatalf("close %s two-local table.copy: %v", name, err)
				}
			}
		})
	}

	ordinary, err := Compile(nil, twoLocalTableCopyModule(false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	staged, err := compileWithFrontendFeatures(cfg, twoLocalTableCopyModule(false, false), features)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(ordinary.Code, staged.Code) {
		t.Fatal("enabling staged two-local table64.copy changed ordinary table32 code bytes")
	}
}

func TestStagedTable64TwoLocalReadWriteMixedWidthsDirectoryAndCodecRoundTrip(t *testing.T) {
	cases := []struct {
		name         string
		table0Addr64 bool
		table1Addr64 bool
	}{
		{name: "table64-table64", table0Addr64: true, table1Addr64: true},
		{name: "table64-table32", table0Addr64: true, table1Addr64: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			module := twoLocalTableReadWriteModule(tc.table0Addr64, tc.table1Addr64)
			compiled, err := compileStagedTable64(module)
			if err != nil {
				t.Fatalf("compile two-local table read/write: %v", err)
			}
			defer compiled.Close()
			meta := (&Module{c: compiled}).Metadata()
			if len(meta.Tables) != 2 || meta.Tables[0].Addr64 != tc.table0Addr64 || meta.Tables[1].Addr64 != tc.table1Addr64 ||
				meta.Tables[0].Min != 2 || meta.Tables[0].Max != 4 || meta.Tables[1].Min != 2 || meta.Tables[1].Max != 4 ||
				!reflect.DeepEqual(meta.Tables[0].Exports, []string{"t0"}) || !reflect.DeepEqual(meta.Tables[1].Exports, []string{"t1"}) {
				t.Fatalf("two-local table read/write metadata = %#v", meta.Tables)
			}
			blob, err := compiled.MarshalBinary()
			if err != nil {
				t.Fatalf("marshal two-local table read/write: %v", err)
			}
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatalf("reload two-local table read/write: %v", err)
			}
			defer loaded.Close()
			loaded.stagedTable64 = true
			loaded.hasTableExportMetadata = true
			if !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
				t.Fatalf("two-local table read/write codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
			}

			for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
				in, err := instantiateCore(c, InstantiateOptions{})
				if err != nil {
					t.Fatalf("instantiate %s two-local table read/write: %v", name, err)
				}
				invoke := func(export string, args ...uint64) uint64 {
					t.Helper()
					got, err := in.Invoke(export, args...)
					if err != nil || len(got) != 1 {
						in.Close()
						t.Fatalf("%s %s%v = %v, err=%v", name, export, args, got, err)
					}
					return got[0]
				}
				if invoke("size0") != 2 || invoke("size1") != 2 {
					in.Close()
					t.Fatalf("%s two-local table sizes are not 2", name)
				}
				if invoke("is_null0", 0) != 0 || invoke("is_null0", 1) != 1 || invoke("is_null1", 0) != 1 || invoke("is_null1", 1) != 0 {
					in.Close()
					t.Fatalf("%s initial two-local table entries are wrong", name)
				}
				if _, err := in.Invoke("set01", 1, 1); err != nil || invoke("is_null0", 1) != 0 {
					in.Close()
					t.Fatalf("%s non-null descriptor write table1->table0: %v", name, err)
				}
				if _, err := in.Invoke("set10", 0, 0); err != nil || invoke("is_null1", 0) != 0 {
					in.Close()
					t.Fatalf("%s non-null descriptor write table0->table1: %v", name, err)
				}
				if _, err := in.Invoke("clear0", 1); err != nil || invoke("is_null0", 1) != 1 {
					in.Close()
					t.Fatalf("%s null descriptor write table0: %v", name, err)
				}
				if _, err := in.Invoke("clear1", 0); err != nil || invoke("is_null1", 0) != 1 {
					in.Close()
					t.Fatalf("%s null descriptor write table1: %v", name, err)
				}
				before0, before1 := invoke("is_null0", 0), invoke("is_null1", 1)
				for _, args := range [][2]uint64{{2, 1}, {1 << 32, 1}, {^uint64(0), 1}} {
					if _, err := in.Invoke("set01", args[0], args[1]); err == nil || !strings.Contains(err.Error(), "out of bounds") {
						in.Close()
						t.Fatalf("%s set01(%d,%d) = %v", name, args[0], args[1], err)
					}
					if invoke("is_null0", 0) != before0 || invoke("is_null1", 1) != before1 {
						in.Close()
						t.Fatalf("%s trapping table.set changed table state", name)
					}
				}
				if !tc.table1Addr64 {
					if invoke("is_null1", 1<<32) != 1 {
						in.Close()
						t.Fatalf("%s table32 index did not retain i32 canonicalization", name)
					}
				} else if _, err := in.Invoke("is_null1", 1<<32); err == nil || !strings.Contains(err.Error(), "out of bounds") {
					in.Close()
					t.Fatalf("%s table64 high index was truncated: %v", name, err)
				}
				if err := in.Close(); err != nil {
					t.Fatalf("close %s two-local table read/write: %v", name, err)
				}
			}
		})
	}

	ordinary, err := Compile(nil, twoLocalTableReadWriteModule(false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	staged, err := compileWithFrontendFeatures(cfg, twoLocalTableReadWriteModule(false, false), features)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(ordinary.Code, staged.Code) {
		t.Fatal("enabling staged two-local table64 read/write changed ordinary table32 code bytes")
	}
}

func TestStagedTable64TwoLocalGrowFillMixedWidthsDirectoryAndCodecRoundTrip(t *testing.T) {
	cases := []struct {
		name         string
		table0Addr64 bool
		table1Addr64 bool
	}{
		{name: "table64-table64", table0Addr64: true, table1Addr64: true},
		{name: "table64-table32", table0Addr64: true, table1Addr64: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			module := twoLocalTableGrowFillModule(tc.table0Addr64, tc.table1Addr64)
			compiled, err := compileStagedTable64(module)
			if err != nil {
				t.Fatalf("compile two-local table grow/fill: %v", err)
			}
			defer compiled.Close()
			meta := (&Module{c: compiled}).Metadata()
			if len(meta.Tables) != 2 || meta.Tables[0].Addr64 != tc.table0Addr64 || meta.Tables[1].Addr64 != tc.table1Addr64 ||
				meta.Tables[0].Min != 2 || meta.Tables[0].Max != 4 || meta.Tables[1].Min != 2 || meta.Tables[1].Max != 4 {
				t.Fatalf("two-local table grow/fill metadata = %#v", meta.Tables)
			}
			blob, err := compiled.MarshalBinary()
			if err != nil {
				t.Fatalf("marshal two-local table grow/fill: %v", err)
			}
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatalf("reload two-local table grow/fill: %v", err)
			}
			defer loaded.Close()
			loaded.stagedTable64 = true
			loaded.hasTableExportMetadata = true
			if !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
				t.Fatalf("two-local table grow/fill codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
			}

			for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
				in, err := instantiateCore(c, InstantiateOptions{})
				if err != nil {
					t.Fatalf("instantiate %s two-local table grow/fill: %v", name, err)
				}
				invoke := func(export string, args ...uint64) uint64 {
					t.Helper()
					got, err := in.Invoke(export, args...)
					if err != nil || len(got) != 1 {
						in.Close()
						t.Fatalf("%s %s%v = %v, err=%v", name, export, args, got, err)
					}
					return got[0]
				}
				if invoke("grow0", 1) != 2 || invoke("grow1", 1) != 2 || invoke("size0") != 3 || invoke("size1") != 3 {
					in.Close()
					t.Fatalf("%s two-local grow result/size mismatch", name)
				}
				if invoke("is_null0", 2) != 0 || invoke("is_null1", 2) != 1 {
					in.Close()
					t.Fatalf("%s grown descriptor initialization mismatch", name)
				}
				if _, err := in.Invoke("fill0", 0, 2); err != nil || invoke("is_null0", 0) != 0 || invoke("is_null0", 1) != 0 {
					in.Close()
					t.Fatalf("%s non-null table64.fill: %v", name, err)
				}
				if _, err := in.Invoke("fill1", 1, 2); err != nil || invoke("is_null1", 1) != 0 || invoke("is_null1", 2) != 0 {
					in.Close()
					t.Fatalf("%s non-null second-table fill: %v", name, err)
				}
				before0, before1 := invoke("is_null0", 2), invoke("is_null1", 0)
				for _, args := range [][2]uint64{{2, 2}, {1 << 32, 0}, {0, 1 << 32}, {^uint64(0), 2}} {
					if _, err := in.Invoke("fill0", args[0], args[1]); err == nil || !strings.Contains(err.Error(), "out of bounds") {
						in.Close()
						t.Fatalf("%s fill0(%d,%d) = %v", name, args[0], args[1], err)
					}
					if invoke("is_null0", 2) != before0 || invoke("is_null1", 0) != before1 {
						in.Close()
						t.Fatalf("%s trapping table.fill changed state", name)
					}
				}
				if invoke("grow0", 1) != 3 || invoke("size0") != 4 {
					in.Close()
					t.Fatalf("%s grow to maximum failed", name)
				}
				for _, delta := range []uint64{1, 1 << 32, ^uint64(0)} {
					if got := invoke("grow0", delta); got != ^uint64(0) {
						in.Close()
						t.Fatalf("%s failed table64.grow(%d) = %d", name, delta, got)
					}
					if invoke("size0") != 4 {
						in.Close()
						t.Fatalf("%s failed table64.grow changed size", name)
					}
				}
				if tc.table1Addr64 {
					if got := invoke("grow1", 1<<32); got != ^uint64(0) {
						in.Close()
						t.Fatalf("%s second table64 high grow delta = %d", name, got)
					}
				} else if invoke("grow1", 1<<32) != 3 || invoke("size1") != 3 {
					in.Close()
					t.Fatalf("%s table32 grow delta did not retain i32 canonicalization", name)
				}
				if err := in.Close(); err != nil {
					t.Fatalf("close %s two-local table grow/fill: %v", name, err)
				}
			}
		})
	}

	ordinary, err := Compile(nil, twoLocalTableGrowFillModule(false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	staged, err := compileWithFrontendFeatures(cfg, twoLocalTableGrowFillModule(false, false), features)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(ordinary.Code, staged.Code) {
		t.Fatal("enabling staged two-local table64 grow/fill changed ordinary table32 code bytes")
	}
}

func TestStagedTable64TwoLocalInitDropMixedWidthsDirectoryAndCodecRoundTrip(t *testing.T) {
	cases := []struct {
		name         string
		table0Addr64 bool
		table1Addr64 bool
	}{
		{name: "table64-table64", table0Addr64: true, table1Addr64: true},
		{name: "table64-table32", table0Addr64: true, table1Addr64: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			module := twoLocalTableInitDropModule(tc.table0Addr64, tc.table1Addr64)
			compiled, err := compileStagedTable64(module)
			if err != nil {
				t.Fatalf("compile two-local table.init/drop: %v", err)
			}
			defer compiled.Close()
			meta := (&Module{c: compiled}).Metadata()
			if len(meta.Tables) != 2 || meta.Tables[0].Addr64 != tc.table0Addr64 || meta.Tables[1].Addr64 != tc.table1Addr64 ||
				meta.Tables[0].Min != 4 || meta.Tables[0].Max != 4 || meta.Tables[1].Min != 4 || meta.Tables[1].Max != 4 {
				t.Fatalf("two-local table.init/drop metadata = %#v", meta.Tables)
			}
			blob, err := compiled.MarshalBinary()
			if err != nil {
				t.Fatalf("marshal two-local table.init/drop: %v", err)
			}
			var loaded Compiled
			if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
				t.Fatalf("reload two-local table.init/drop: %v", err)
			}
			defer loaded.Close()
			loaded.stagedTable64 = true
			loaded.hasTableExportMetadata = true
			if !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) || len(loaded.passiveElems) != 3 || len(loaded.passiveElems[0].Values) != 3 || len(loaded.passiveElems[1].Values) != 3 || len(loaded.passiveElems[2].Values) != 0 {
				t.Fatalf("two-local table.init/drop codec state = tables %#v elems %#v", (&Module{c: &loaded}).Metadata().Tables, loaded.passiveElems)
			}

			for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
				in, err := instantiateCore(c, InstantiateOptions{})
				if err != nil {
					t.Fatalf("instantiate %s two-local table.init/drop: %v", name, err)
				}
				isNull := func(table int, index uint64) uint64 {
					t.Helper()
					got, err := in.Invoke("is_null"+string(rune('0'+table)), index)
					if err != nil || len(got) != 1 {
						in.Close()
						t.Fatalf("%s table %d get(%d) = %v, err=%v", name, table, index, got, err)
					}
					return got[0]
				}
				if _, err := in.Invoke("init0", 0, 0, 3); err != nil || isNull(0, 0) != 0 || isNull(0, 1) != 1 || isNull(0, 2) != 0 {
					in.Close()
					t.Fatalf("%s table0 passive segment identity: %v", name, err)
				}
				if _, err := in.Invoke("init1", 0, 0, 3); err != nil || isNull(1, 0) != 1 || isNull(1, 1) != 0 || isNull(1, 2) != 1 {
					in.Close()
					t.Fatalf("%s table1 passive segment identity: %v", name, err)
				}
				before0, before1 := isNull(0, 3), isNull(1, 3)
				for _, args := range [][3]uint64{{3, 0, 2}, {0, 2, 2}, {^uint64(0), 0, 2}} {
					if _, err := in.Invoke("init0", args[0], args[1], args[2]); err == nil || !strings.Contains(err.Error(), "out of bounds") {
						in.Close()
						t.Fatalf("%s init0(%d,%d,%d) = %v", name, args[0], args[1], args[2], err)
					}
					if isNull(0, 3) != before0 || isNull(1, 3) != before1 {
						in.Close()
						t.Fatalf("%s trapping table.init changed state", name)
					}
				}
				if _, err := in.Invoke("drop0"); err != nil {
					in.Close()
					t.Fatalf("%s elem.drop 0: %v", name, err)
				}
				if _, err := in.Invoke("init0", 4, 0, 0); err != nil {
					in.Close()
					t.Fatalf("%s zero-length init after drop: %v", name, err)
				}
				if _, err := in.Invoke("init0", 0, 0, 1); err == nil {
					in.Close()
					t.Fatalf("%s nonzero init after drop succeeded", name)
				}
				if _, err := in.Invoke("init1", 3, 1, 1); err != nil || isNull(1, 3) != 0 {
					in.Close()
					t.Fatalf("%s dropping segment 0 changed segment 1 identity: %v", name, err)
				}
				if _, err := in.Invoke("drop1"); err != nil {
					in.Close()
					t.Fatalf("%s elem.drop 1: %v", name, err)
				}
				if _, err := in.Invoke("init1", 4, 0, 0); err != nil {
					in.Close()
					t.Fatalf("%s zero-length second init after drop: %v", name, err)
				}
				if _, err := in.Invoke("init_decl", 4, 0, 0); err != nil {
					in.Close()
					t.Fatalf("%s zero-length declarative init: %v", name, err)
				}
				if _, err := in.Invoke("init_decl", 0, 0, 1); err == nil {
					in.Close()
					t.Fatalf("%s nonzero declarative init succeeded", name)
				}
				if _, err := in.Invoke("drop_decl"); err != nil {
					in.Close()
					t.Fatalf("%s declarative elem.drop: %v", name, err)
				}
				if !tc.table1Addr64 {
					if _, err := in.Invoke("init1", 1<<32, 0, 0); err != nil {
						in.Close()
						t.Fatalf("%s table32 init destination did not retain i32 canonicalization: %v", name, err)
					}
				} else if _, err := in.Invoke("init1", 1<<32, 0, 0); err == nil || !strings.Contains(err.Error(), "out of bounds") {
					in.Close()
					t.Fatalf("%s table64 high init destination was truncated: %v", name, err)
				}
				if err := in.Close(); err != nil {
					t.Fatalf("close %s two-local table.init/drop: %v", name, err)
				}
			}
		})
	}

	ordinary, err := Compile(nil, twoLocalTableInitDropModule(false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	staged, err := compileWithFrontendFeatures(cfg, twoLocalTableInitDropModule(false, false), features)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(ordinary.Code, staged.Code) {
		t.Fatal("enabling staged two-local table64.init/drop changed ordinary table32 code bytes")
	}
}

func twoLocalExternrefTable64ReadWriteModule(t testing.TB) []byte {
	return watToWasm(t, `(module
		(table $ext i64 2 externref)
		(table $fun i64 3 funcref)
		(elem (table $fun) (i64.const 1) func $dummy)
		(func $dummy)
		(func (export "get-ext") (param i64) (result externref)
			(table.get $ext (local.get 0)))
		(func (export "set-ext") (param i64 externref)
			(table.set $ext (local.get 0) (local.get 1)))
		(func (export "fun-null") (param i64) (result i32)
			(ref.is_null (table.get $fun (local.get 0))))
		(func (export "copy-fun") (param i64 i64)
			(table.set $fun (local.get 0) (table.get $fun (local.get 1))))
	)`)
}

func TestStagedTable64TwoLocalExternrefReadWriteIdentityAtomicityAndCodecRoundTrip(t *testing.T) {
	module := twoLocalExternrefTable64ReadWriteModule(t)
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile two-local externref table64 read/write: %v", err)
	}
	defer compiled.Close()
	meta := (&Module{c: compiled}).Metadata()
	if len(meta.Tables) != 2 || meta.Tables[0].Type != ValExternRef || !meta.Tables[0].Addr64 || meta.Tables[0].Min != 2 || meta.Tables[0].HasMax || meta.Tables[1].Type != ValFuncRef || !meta.Tables[1].Addr64 || meta.Tables[1].Min != 3 || meta.Tables[1].HasMax {
		t.Fatalf("two-local externref table64 metadata = %#v", meta.Tables)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal two-local externref table64: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload two-local externref table64: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true
	loaded.hasTableExportMetadata = true
	if !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
		t.Fatalf("two-local externref table64 codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
	}

	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s two-local externref table64: %v", name, err)
		}
		refA := issueExternref(t, in, name+"-a")
		refB := issueExternref(t, in, name+"-b")
		if _, err := in.Call(context.Background(), "set-ext", ValueI64(1), ValueExternRef(refA)); err != nil {
			in.Close()
			t.Fatalf("%s table64 externref set: %v", name, err)
		}
		got, err := in.Call(context.Background(), "get-ext", ValueI64(1))
		if err != nil || len(got) != 1 || got[0].ExternRef() != refA || resolveExternref(t, in, got[0].ExternRef()) != name+"-a" {
			in.Close()
			t.Fatalf("%s table64 externref identity = %v, err=%v", name, got, err)
		}
		for _, index := range []uint64{2, 1 << 32, ^uint64(0)} {
			if _, err := in.Call(context.Background(), "set-ext", ValueI64(int64(index)), ValueExternRef(refB)); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				in.Close()
				t.Fatalf("%s table64 externref set(%d) error = %v", name, index, err)
			}
			got, err = in.Call(context.Background(), "get-ext", ValueI64(1))
			if err != nil || got[0].ExternRef() != refA {
				in.Close()
				t.Fatalf("%s trapping table64 externref set changed entry = %v, err=%v", name, got, err)
			}
		}
		if _, err := in.Call(context.Background(), "set-ext", ValueI64(1), ValueExternRef(NullExternRef())); err != nil {
			in.Close()
			t.Fatalf("%s table64 externref null set: %v", name, err)
		}
		got, err = in.Call(context.Background(), "get-ext", ValueI64(1))
		if err != nil || !got[0].ExternRef().IsNull() {
			in.Close()
			t.Fatalf("%s table64 externref null get = %v, err=%v", name, got, err)
		}
		if got, err := in.Invoke("fun-null", 1); err != nil || len(got) != 1 || got[0] != 0 {
			in.Close()
			t.Fatalf("%s table64 funcref active entry = %v, err=%v", name, got, err)
		}
		if _, err := in.Invoke("copy-fun", 2, 1); err != nil {
			in.Close()
			t.Fatalf("%s table64 funcref copy by get/set: %v", name, err)
		}
		if got, err := in.Invoke("fun-null", 2); err != nil || got[0] != 0 {
			in.Close()
			t.Fatalf("%s table64 funcref copied entry = %v, err=%v", name, got, err)
		}
		if got, want := len(in.tableDescriptor(0)), 8+2*8; got != want {
			in.Close()
			t.Fatalf("%s externref table64 descriptor = %d bytes, want bounded minimum-only %d", name, got, want)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s two-local externref table64: %v", name, err)
		}
	}
	grow := watToWasm(t, `(module
		(table i64 1 externref)
		(table i64 1 funcref)
		(func (param i64) (drop (table.grow 0 (ref.null extern) (local.get 0))))
	)`)
	if c, err := compileStagedTable64(grow); err == nil || !strings.Contains(err.Error(), "outside the exact table operation slice") {
		if c != nil {
			c.Close()
		}
		t.Fatalf("two-local externref table64 grow gate = %v", err)
	}
}

func twoLocalExternrefMixedFillModule(t testing.TB) []byte {
	return watToWasm(t, `(module
		(table $t32 10 externref)
		(table $t64 i64 10 externref)
		(func (export "get32") (param i32) (result externref)
			(table.get $t32 (local.get 0)))
		(func (export "get64") (param i64) (result externref)
			(table.get $t64 (local.get 0)))
		(func (export "fill32") (param i32 externref i32)
			(table.fill $t32 (local.get 0) (local.get 1) (local.get 2)))
		(func (export "fill64") (param i64 externref i64)
			(table.fill $t64 (local.get 0) (local.get 1) (local.get 2)))
	)`)
}

func TestStagedTable64MixedExternrefFillWidthsAtomicityAndCodecRoundTrip(t *testing.T) {
	module := twoLocalExternrefMixedFillModule(t)
	compiled, err := compileStagedTable64(module)
	if err != nil {
		t.Fatalf("compile mixed externref table64 fill: %v", err)
	}
	defer compiled.Close()
	meta := (&Module{c: compiled}).Metadata()
	if len(meta.Tables) != 2 || meta.Tables[0].Type != ValExternRef || meta.Tables[0].Addr64 || meta.Tables[0].Min != 10 || meta.Tables[0].HasMax || meta.Tables[1].Type != ValExternRef || !meta.Tables[1].Addr64 || meta.Tables[1].Min != 10 || meta.Tables[1].HasMax {
		t.Fatalf("mixed externref table64 fill metadata = %#v", meta.Tables)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal mixed externref table64 fill: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload mixed externref table64 fill: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true
	loaded.hasTableExportMetadata = true
	if !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
		t.Fatalf("mixed externref table64 fill codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
	}

	for name, c := range map[string]*Compiled{"source": compiled, "codec": &loaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s mixed externref table64 fill: %v", name, err)
		}
		refA := issueExternref(t, in, name+"-fill-a")
		refB := issueExternref(t, in, name+"-fill-b")
		if _, err := in.Call(context.Background(), "fill32", ValueI32(2), ValueExternRef(refA), ValueI32(3)); err != nil {
			in.Close()
			t.Fatalf("%s table32 externref fill: %v", name, err)
		}
		if _, err := in.Call(context.Background(), "fill64", ValueI64(2), ValueExternRef(refB), ValueI64(3)); err != nil {
			in.Close()
			t.Fatalf("%s table64 externref fill: %v", name, err)
		}
		for _, tc := range []struct {
			get   string
			index Value
			want  ExternRef
		}{{"get32", ValueI32(2), refA}, {"get32", ValueI32(4), refA}, {"get64", ValueI64(2), refB}, {"get64", ValueI64(4), refB}} {
			got, err := in.Call(context.Background(), tc.get, tc.index)
			if err != nil || len(got) != 1 || got[0].ExternRef() != tc.want {
				in.Close()
				t.Fatalf("%s %s identity = %v, err=%v, want %v", name, tc.get, got, err, tc.want)
			}
		}
		tokenA := ValueExternRef(refA).Bits()
		if _, err := in.Invoke("fill32", uint64(1)<<32|5, tokenA, uint64(1)<<32|1); err != nil {
			in.Close()
			t.Fatalf("%s table32 externref fill canonicalization: %v", name, err)
		}
		if got, err := in.Call(context.Background(), "get32", ValueI32(5)); err != nil || got[0].ExternRef() != refA {
			in.Close()
			t.Fatalf("%s table32 canonicalized fill entry = %v, err=%v", name, got, err)
		}
		if _, err := in.Call(context.Background(), "fill64", ValueI64(9), ValueExternRef(NullExternRef()), ValueI64(1)); err != nil {
			in.Close()
			t.Fatalf("%s table64 externref null fill: %v", name, err)
		}
		if got, err := in.Call(context.Background(), "get64", ValueI64(9)); err != nil || !got[0].ExternRef().IsNull() {
			in.Close()
			t.Fatalf("%s table64 externref null fill result = %v, err=%v", name, got, err)
		}
		if _, err := in.Call(context.Background(), "fill64", ValueI64(10), ValueExternRef(refA), ValueI64(0)); err != nil {
			in.Close()
			t.Fatalf("%s table64 externref zero fill at boundary: %v", name, err)
		}
		for _, args := range [][2]uint64{{8, 3}, {11, 0}, {1 << 32, 0}, {^uint64(0), 2}} {
			if _, err := in.Call(context.Background(), "fill64", ValueI64(int64(args[0])), ValueExternRef(refA), ValueI64(int64(args[1]))); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				in.Close()
				t.Fatalf("%s table64 externref fill(%d,%d) error = %v", name, args[0], args[1], err)
			}
			if got, err := in.Call(context.Background(), "get64", ValueI64(4)); err != nil || got[0].ExternRef() != refB {
				in.Close()
				t.Fatalf("%s trapping table64 externref fill changed entry = %v, err=%v", name, got, err)
			}
		}
		for tableIndex := 0; tableIndex < 2; tableIndex++ {
			if got, want := len(in.tableDescriptor(tableIndex)), 8+10*8; got != want {
				in.Close()
				t.Fatalf("%s externref table %d descriptor = %d bytes, want minimum-only %d", name, tableIndex, got, want)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s mixed externref table64 fill: %v", name, err)
		}
	}
}

func soleExternrefTable64GrowModule(t testing.TB) []byte {
	return watToWasm(t, `(module
		(table $t i64 0 externref)
		(func (export "get") (param i64) (result externref) (table.get $t (local.get 0)))
		(func (export "set") (param i64 externref) (table.set $t (local.get 0) (local.get 1)))
		(func (export "grow") (param i64 externref) (result i64)
			(table.grow $t (local.get 1) (local.get 0)))
		(func (export "size") (result i64) (table.size $t))
	)`)
}

func fourLocalExternrefTable64SizeGrowModule(t testing.TB) []byte {
	return watToWasm(t, `(module
		(table $t0 i64 0 externref)
		(table $t1 i64 1 externref)
		(table $t2 i64 0 2 externref)
		(table $t3 i64 3 8 externref)
		(func (export "size0") (result i64) (table.size $t0))
		(func (export "size1") (result i64) (table.size $t1))
		(func (export "size2") (result i64) (table.size $t2))
		(func (export "size3") (result i64) (table.size $t3))
		(func (export "grow0") (param i64) (result i64) (table.grow $t0 (ref.null extern) (local.get 0)))
		(func (export "grow1") (param i64) (result i64) (table.grow $t1 (ref.null extern) (local.get 0)))
		(func (export "grow2") (param i64) (result i64) (table.grow $t2 (ref.null extern) (local.get 0)))
		(func (export "grow3") (param i64) (result i64) (table.grow $t3 (ref.null extern) (local.get 0)))
	)`)
}

func TestStagedTable64ExternrefGrowAndFourLocalSizeDirectoryCodecRoundTrip(t *testing.T) {
	sole, err := compileStagedTable64(soleExternrefTable64GrowModule(t))
	if err != nil {
		t.Fatalf("compile sole externref table64 grow: %v", err)
	}
	defer sole.Close()
	four, err := compileStagedTable64(fourLocalExternrefTable64SizeGrowModule(t))
	if err != nil {
		t.Fatalf("compile four-local externref table64 size/grow: %v", err)
	}
	defer four.Close()
	fourMeta := (&Module{c: four}).Metadata()
	if len(fourMeta.Tables) != 4 || fourMeta.Tables[0].Min != 0 || fourMeta.Tables[0].HasMax || fourMeta.Tables[1].Min != 1 || fourMeta.Tables[1].HasMax || fourMeta.Tables[2].Min != 0 || !fourMeta.Tables[2].HasMax || fourMeta.Tables[2].Max != 2 || fourMeta.Tables[3].Min != 3 || !fourMeta.Tables[3].HasMax || fourMeta.Tables[3].Max != 8 {
		t.Fatalf("four-local externref table64 metadata = %#v", fourMeta.Tables)
	}
	for i := range fourMeta.Tables {
		if fourMeta.Tables[i].Type != ValExternRef || !fourMeta.Tables[i].Addr64 {
			t.Fatalf("four-local externref table64 metadata[%d] = %#v", i, fourMeta.Tables[i])
		}
	}

	reload := func(name string, c *Compiled) *Compiled {
		t.Helper()
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal %s externref table64: %v", name, err)
		}
		loaded := new(Compiled)
		if err := unmarshalCompiled(loaded, blob[5:]); err != nil {
			t.Fatalf("reload %s externref table64: %v", name, err)
		}
		loaded.stagedTable64 = true
		loaded.hasTableExportMetadata = true
		return loaded
	}
	soleLoaded := reload("sole", sole)
	defer soleLoaded.Close()
	fourLoaded := reload("four-local", four)
	defer fourLoaded.Close()
	if !reflect.DeepEqual((&Module{c: fourLoaded}).Metadata().Tables, fourMeta.Tables) {
		t.Fatalf("four-local externref table64 codec metadata = %#v, want %#v", (&Module{c: fourLoaded}).Metadata().Tables, fourMeta.Tables)
	}

	for name, c := range map[string]*Compiled{"source": sole, "codec": soleLoaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s sole externref table64 grow: %v", name, err)
		}
		refA := issueExternref(t, in, name+"-grow-a")
		refB := issueExternref(t, in, name+"-grow-b")
		if got, err := in.Call(context.Background(), "grow", ValueI64(1), ValueExternRef(refA)); err != nil || len(got) != 1 || got[0].I64() != 0 {
			in.Close()
			t.Fatalf("%s sole externref table64 grow(1) = %v, err=%v", name, got, err)
		}
		if got, err := in.Call(context.Background(), "get", ValueI64(0)); err != nil || got[0].ExternRef() != refA {
			in.Close()
			t.Fatalf("%s sole externref table64 grown token = %v, err=%v", name, got, err)
		}
		if got, err := in.Call(context.Background(), "grow", ValueI64(4), ValueExternRef(refB)); err != nil || got[0].I64() != 1 {
			in.Close()
			t.Fatalf("%s sole externref table64 grow(4) = %v, err=%v", name, got, err)
		}
		if got, err := in.Call(context.Background(), "get", ValueI64(4)); err != nil || got[0].ExternRef() != refB {
			in.Close()
			t.Fatalf("%s sole externref table64 grown range token = %v, err=%v", name, got, err)
		}
		for _, delta := range []uint64{1 << 32, ^uint64(0)} {
			if got, err := in.Call(context.Background(), "grow", ValueI64(int64(delta)), ValueExternRef(refA)); err != nil || len(got) != 1 || got[0].Bits() != ^uint64(0) {
				in.Close()
				t.Fatalf("%s sole externref table64 grow(%d) = %v, err=%v, want -1", name, delta, got, err)
			}
			if got, err := in.Invoke("size"); err != nil || got[0] != 5 {
				in.Close()
				t.Fatalf("%s failed sole externref table64 grow changed size = %v, err=%v", name, got, err)
			}
		}
		if got, want := len(in.tableDescriptor(0)), 8+1024*8; got != want {
			in.Close()
			t.Fatalf("%s sole externref table64 descriptor = %d bytes, want bounded %d", name, got, want)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s sole externref table64 grow: %v", name, err)
		}
	}

	for name, c := range map[string]*Compiled{"source": four, "codec": fourLoaded} {
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate %s four-local externref table64: %v", name, err)
		}
		for i, want := range []uint64{0, 1, 0, 3} {
			if got, err := in.Invoke("size" + string(rune('0'+i))); err != nil || len(got) != 1 || got[0] != want {
				in.Close()
				t.Fatalf("%s four-local table%d size = %v, err=%v, want %d", name, i, got, err, want)
			}
		}
		for _, tc := range []struct {
			name             string
			delta, old, size uint64
		}{{"grow0", 5, 0, 5}, {"grow1", 5, 1, 6}, {"grow2", 3, ^uint64(0), 0}, {"grow2", 2, 0, 2}, {"grow3", 4, 3, 7}, {"grow3", 2, ^uint64(0), 7}, {"grow3", 1, 7, 8}} {
			got, err := in.Invoke(tc.name, tc.delta)
			if err != nil || len(got) != 1 || got[0] != tc.old {
				in.Close()
				t.Fatalf("%s %s(%d) = %v, err=%v, want old %d", name, tc.name, tc.delta, got, err, tc.old)
			}
			tableIndex := int(tc.name[len(tc.name)-1] - '0')
			if got, err := in.Invoke("size" + string(rune('0'+tableIndex))); err != nil || got[0] != tc.size {
				in.Close()
				t.Fatalf("%s table%d size after %s = %v, err=%v, want %d", name, tableIndex, tc.name, got, err, tc.size)
			}
		}
		for i, capacity := range []int{1024, 1024, 2, 8} {
			if got, want := len(in.tableDescriptor(i)), 8+capacity*8; got != want {
				in.Close()
				t.Fatalf("%s four-local table%d descriptor = %d bytes, want %d", name, i, got, want)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatalf("close %s four-local externref table64: %v", name, err)
		}
	}
}

func TestStagedTable64InstanceExportImportLifecycle(t *testing.T) {
	max4 := uint64(4)
	ownerCompiled, err := compileStagedTable64(table64LifecycleModule(&max4))
	if err != nil {
		t.Fatalf("compile bounded table64 owner: %v", err)
	}
	defer ownerCompiled.Close()
	owner, err := instantiateCore(ownerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate bounded table64 owner: %v", err)
	}
	table, err := owner.ExportedTable("table")
	if err != nil {
		t.Fatalf("export bounded table64: %v", err)
	}

	consumerCompiled, err := compileStagedTable64(table64ImportLifecycleModule(2, &max4))
	if err != nil {
		t.Fatalf("compile bounded table64 consumer: %v", err)
	}
	defer consumerCompiled.Close()
	meta := (&Module{c: consumerCompiled}).Metadata()
	if len(meta.Tables) != 1 || meta.Tables[0].ImportModule != "env" || meta.Tables[0].ImportName != "table" || !meta.Tables[0].Addr64 || meta.Tables[0].Min != 2 || meta.Tables[0].Max != 4 || !meta.Tables[0].HasMax || !reflect.DeepEqual(meta.Tables[0].Exports, []string{"table"}) {
		t.Fatalf("table64 import metadata = %#v", meta.Tables)
	}
	rt := &Runtime{imports: Imports{}}
	imports := rt.buildModule(consumerCompiled).Imports()
	if len(imports) != 1 || imports[0].Kind != ImportTable || !imports[0].Addr64 || imports[0].Min != 2 || imports[0].Max != 4 || !imports[0].HasMax {
		t.Fatalf("table64 import inspection = %#v", imports)
	}
	if err := applyPolicy(&Module{c: consumerCompiled}, Policy{MaxTableEntries: 2}); err != nil {
		t.Fatalf("table64 import exact policy: %v", err)
	}
	if err := applyPolicy(&Module{c: consumerCompiled}, Policy{MaxTableEntries: 1}); err == nil {
		t.Fatal("table64 import minimum above policy limit was accepted")
	}
	blob, err := consumerCompiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal table64 import: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("reload table64 import: %v", err)
	}
	defer loaded.Close()
	loaded.stagedTable64 = true
	loaded.hasTableExportMetadata = true
	if !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Tables, meta.Tables) {
		t.Fatalf("table64 import codec metadata = %#v, want %#v", (&Module{c: &loaded}).Metadata().Tables, meta.Tables)
	}
	if _, err := Capture(consumerCompiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "tables cannot be snapshotted") {
		t.Fatalf("imported table64 snapshot = %v", err)
	}

	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.table": table}})
	if err != nil {
		t.Fatalf("instantiate bounded table64 consumer: %v", err)
	}
	if got, err := consumer.Invoke("size"); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("imported table64.size = %v, err=%v", got, err)
	}
	if got, err := consumer.Invoke("grow", 1); err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("imported table64.grow = %v, err=%v", got, err)
	}
	if got, err := owner.Invoke("size"); err != nil || got[0] != 3 || table.Size() != 3 {
		t.Fatalf("table64 grow visibility owner=%v handle=%d err=%v", got, table.Size(), err)
	}
	reexported, err := consumer.ExportedTable("table")
	if err != nil {
		t.Fatalf("re-export imported table64: %v", err)
	}
	loadedIn, err := instantiateCore(&loaded, InstantiateOptions{Imports: Imports{"env.table": reexported}})
	if err != nil {
		t.Fatalf("instantiate codec-reloaded table64 consumer: %v", err)
	}
	if got, err := loadedIn.Invoke("size"); err != nil || got[0] != 3 {
		t.Fatalf("codec/re-export table64 size = %v, err=%v", got, err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("logical close table64 producer: %v", err)
	}
	owner.lifeMu.Lock()
	closed := owner.resourcesClosed
	owner.lifeMu.Unlock()
	if closed {
		t.Fatal("table64 producer resources closed while consumers retained export")
	}
	if got, err := consumer.Invoke("size"); err != nil || got[0] != 3 {
		t.Fatalf("retained table64 import after producer close = %v, err=%v", got, err)
	}
	if got, err := loadedIn.Invoke("size"); err != nil || got[0] != 3 {
		t.Fatalf("retained codec/re-export table64 after producer close = %v, err=%v", got, err)
	}
	if err := loadedIn.Close(); err != nil {
		t.Fatalf("close codec-reloaded table64 consumer: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("close table64 consumer: %v", err)
	}
	owner.lifeMu.Lock()
	closed = owner.resourcesClosed
	owner.lifeMu.Unlock()
	if !closed {
		t.Fatal("table64 producer resources remained after final consumer close")
	}
}

func TestStagedTable64ImportLimitCompatibilityAndRollback(t *testing.T) {
	instantiateOwner := func(t *testing.T, max *uint64) (*Compiled, *Instance, *Table) {
		t.Helper()
		compiled, err := compileStagedTable64(table64LifecycleModule(max))
		if err != nil {
			t.Fatal(err)
		}
		owner, err := instantiateCore(compiled, InstantiateOptions{})
		if err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		table, err := owner.ExportedTable("table")
		if err != nil {
			owner.Close()
			compiled.Close()
			t.Fatal(err)
		}
		return compiled, owner, table
	}
	max4, max5, max3 := uint64(4), uint64(5), uint64(3)
	boundedCompiled, boundedOwner, bounded := instantiateOwner(t, &max4)
	defer boundedCompiled.Close()
	defer boundedOwner.Close()
	for name, module := range map[string][]byte{
		"no maximum import accepts bounded provider": table64ImportLifecycleModule(1, nil),
		"wider maximum accepts bounded provider":     table64ImportLifecycleModule(1, &max5),
	} {
		c, err := compileStagedTable64(module)
		if err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
		in, err := instantiateCore(c, InstantiateOptions{Imports: Imports{"env.table": bounded}})
		if err != nil {
			c.Close()
			t.Fatalf("%s: %v", name, err)
		}
		in.Close()
		c.Close()
	}
	for name, module := range map[string][]byte{
		"minimum": table64ImportLifecycleModule(3, &max4),
		"maximum": table64ImportLifecycleModule(1, &max3),
	} {
		c, err := compileStagedTable64(module)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := instantiateCore(c, InstantiateOptions{Imports: Imports{"env.table": bounded}}); err == nil {
			c.Close()
			t.Fatalf("%s mismatch was accepted", name)
		}
		if boundedOwner.hasResourceRoots() {
			c.Close()
			t.Fatalf("%s mismatch retained table64 producer", name)
		}
		c.Close()
	}

	unboundedCompiled, unboundedOwner, unbounded := instantiateOwner(t, nil)
	defer unboundedCompiled.Close()
	defer unboundedOwner.Close()
	if unboundedCompiled.TableHasMax || unboundedCompiled.TableMax != int(frontend.StagedTable64Max()) {
		t.Fatalf("no-max exported table64 runtime reservation = max %d hasMax %v", unboundedCompiled.TableMax, unboundedCompiled.TableHasMax)
	}
	noMaxConsumer, err := compileStagedTable64(table64ImportLifecycleModule(1, nil))
	if err != nil {
		t.Fatal(err)
	}
	noMaxIn, err := instantiateCore(noMaxConsumer, InstantiateOptions{Imports: Imports{"env.table": unbounded}})
	if err != nil {
		noMaxConsumer.Close()
		t.Fatalf("no-max table64 import: %v", err)
	}
	reexported, err := noMaxIn.ExportedTable("table")
	if err != nil {
		noMaxIn.Close()
		noMaxConsumer.Close()
		t.Fatal(err)
	}
	boundedImport, err := compileStagedTable64(table64ImportLifecycleModule(1, &max5))
	if err != nil {
		noMaxIn.Close()
		noMaxConsumer.Close()
		t.Fatal(err)
	}
	for name, provider := range map[string]*Table{"owner": unbounded, "re-export": reexported} {
		if _, err := instantiateCore(boundedImport, InstantiateOptions{Imports: Imports{"env.table": provider}}); err == nil || !strings.Contains(err.Error(), "no declared maximum") {
			boundedImport.Close()
			noMaxIn.Close()
			noMaxConsumer.Close()
			t.Fatalf("bounded import of %s no-max table64 provider = %v", name, err)
		}
	}
	boundedImport.Close()
	noMaxIn.Close()
	noMaxConsumer.Close()

	host, err := NewTable(2, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	consumer, err := compileStagedTable64(table64ImportLifecycleModule(1, &max4))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if _, err := instantiateCore(consumer, InstantiateOptions{Imports: Imports{"env.table": host}}); err == nil || !strings.Contains(err.Error(), "provider is table32, import requires table64") {
		t.Fatalf("host table32 into table64 import = %v", err)
	}
}

func TestStagedTable64GatesAndTable32CodeStability(t *testing.T) {
	unboundedGrow := table64LifecycleModule(nil)
	unbounded, err := compileStagedTable64(unboundedGrow)
	if err != nil {
		t.Fatalf("bounded-reservation no-max table64: %v", err)
	}
	if unbounded.TableHasMax || unbounded.TableMax != int(frontend.StagedTable64Max()) {
		unbounded.Close()
		t.Fatalf("no-max table64 runtime shape = max %d hasMax %v", unbounded.TableMax, unbounded.TableHasMax)
	}
	unbounded.Close()
	if _, err := compileStagedTable64(boundedTable64Module(16385)); err == nil || !strings.Contains(err.Error(), "16384") {
		t.Fatalf("oversized table64 error = %v", err)
	}
	imported, err := compileStagedTable64(table64ImportLifecycleModule(1, nil))
	if err != nil {
		t.Fatalf("bounded table64 import compile: %v", err)
	}
	imported.Close()
	importedCopyEntry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	importedCopyEntry = append(importedCopyEntry, byte(wasm.ExternTable), 0x70, 0x05, 0x01, 0x04)
	importedCopy := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil))),
		wasmtest.Section(2, wasmtest.Vec(importedCopyEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x00, 0x0b}))),
	)
	if _, err := compileStagedTable64(importedCopy); err == nil || !strings.Contains(err.Error(), "imported table64") {
		t.Fatalf("imported table64.copy gate = %v", err)
	}
	producerCompiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		t.Fatal(err)
	}
	defer producerCompiled.Close()
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	table64, err := producer.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	memory32Import := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	memory32Import = append(memory32Import, byte(wasm.ExternTable), 0x70, 0x01, 0x02, 0x04)
	memory32Consumer := MustCompile(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(memory32Import))))
	defer memory32Consumer.Close()
	if _, err := instantiateCore(memory32Consumer, InstantiateOptions{Imports: Imports{"env.table": table64}}); err == nil || !strings.Contains(err.Error(), "provider is table64, import requires table32") {
		t.Fatalf("table64 provider into table32 import = %v", err)
	}
	table := []byte{0x70, 0x05, 0x01, 0x02}
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(table, table, table)))); err == nil || !strings.Contains(err.Error(), "exactly one local/imported table") {
		t.Fatalf("three-table table64 error = %v", err)
	}
	twoWithoutCopy := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(table, table)))
	if _, err := compileStagedTable64(twoWithoutCopy); err == nil || !strings.Contains(err.Error(), "requires a table operation") {
		t.Fatalf("two-table table64 without operation gate = %v", err)
	}
	twoWithSet := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec(table, table)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x26, 0x01, 0x0b}))),
	)
	twoWithSetCompiled, err := compileStagedTable64(twoWithSet)
	if err != nil {
		t.Fatalf("two-table table64 set admission: %v", err)
	}
	twoWithSetCompiled.Close()
	noMaxTable := []byte{0x70, 0x04, 0x01}
	noMaxCopy := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec(noMaxTable, table)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x01, 0x0b}))),
	)
	if _, err := compileStagedTable64(noMaxCopy); err == nil || !strings.Contains(err.Error(), "explicit maximum") {
		t.Fatalf("two-table no-max table64.copy gate = %v", err)
	}
	externref := []byte{0x6f, 0x05, 0x01, 0x02}
	if _, err := compileStagedTable64(wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(externref)))); err == nil || !strings.Contains(err.Error(), "funcref") {
		t.Fatalf("externref table64 error = %v", err)
	}
	passive := tableInitDropModule(true)
	passiveCompiled, err := compileStagedTable64(passive)
	if err != nil {
		t.Fatalf("sole-local passive table64 element lifecycle: %v", err)
	}
	passiveCompiled.Close()
	importedInitEntry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	importedInitEntry = append(importedInitEntry, byte(wasm.ExternTable), 0x70, 0x05, 0x01, 0x04)
	importedInit := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(2, wasmtest.Vec(importedInitEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElem())),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0c, 0x00, 0x00, 0x0b}))),
	)
	if _, err := compileStagedTable64(importedInit); err == nil || !strings.Contains(err.Error(), "imported table64") {
		t.Fatalf("imported table64.init gate = %v", err)
	}
	cfg := NewRuntimeConfig()
	cfg.boundsChecks = BoundsChecksSignalsBased
	features := cfg.frontendFeatures()
	features.Table64 = true
	if _, err := compileWithFrontendFeatures(cfg, boundedTable64Module(4), features); err == nil || !strings.Contains(err.Error(), "signals-based") {
		t.Fatalf("guard table64 error = %v", err)
	}

	ordinary := table32GetSetGrowSizeFillModule()
	base, err := Compile(nil, ordinary)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	stageCfg := NewRuntimeConfig()
	stageFeatures := stageCfg.frontendFeatures()
	stageFeatures.Table64 = true
	staged, err := compileWithFrontendFeatures(stageCfg, ordinary, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(base.Code, staged.Code) {
		t.Fatal("enabling staged table64 changed table32 code bytes")
	}
	ordinaryIndirect := table32CallIndirectModule()
	baseIndirect, err := Compile(nil, ordinaryIndirect)
	if err != nil {
		t.Fatal(err)
	}
	defer baseIndirect.Close()
	stagedIndirect, err := compileWithFrontendFeatures(stageCfg, ordinaryIndirect, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer stagedIndirect.Close()
	if !bytes.Equal(baseIndirect.Code, stagedIndirect.Code) {
		t.Fatal("enabling staged table64 changed table32 call_indirect code bytes")
	}
}

func BenchmarkStagedTable64ExternrefGrowZero(b *testing.B) {
	compiled, err := compileStagedTable64(soleExternrefTable64GrowModule(b))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("grow", 0, 0); err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("table64 externref grow(0) = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64ExternrefFillZero(b *testing.B) {
	compiled, err := compileStagedTable64(twoLocalExternrefMixedFillModule(b))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("fill64", 10, 0, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64ExternrefGet(b *testing.B) {
	compiled, err := compileStagedTable64(twoLocalExternrefTable64ReadWriteModule(b))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("get-ext", 0); err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("table64 externref get = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64InitZero(b *testing.B) {
	compiled, err := compileStagedTable64(tableInitDropModule(true))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("init", 4, 3, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64TwoLocalSize(b *testing.B) {
	compiled, err := compileStagedTable64(twoLocalTableReadWriteModule(true, true))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("size1"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64TwoLocalGrowZero(b *testing.B) {
	compiled, err := compileStagedTable64(twoLocalTableGrowFillModule(true, true))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("grow1", 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64TwoLocalInitZero(b *testing.B) {
	compiled, err := compileStagedTable64(twoLocalTableInitDropModule(true, true))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("init1", 4, 0, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64TwoLocalCopyZero(b *testing.B) {
	compiled, err := compileStagedTable64(twoLocalTableCopyModule(true, true))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("copy10", 4, 4, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64CopyZero(b *testing.B) {
	compiled, err := compileStagedTable64(table64CopyModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("copy", 4, 4, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64ImportedSize(b *testing.B) {
	max4 := uint64(4)
	ownerCompiled, err := compileStagedTable64(table64LifecycleModule(&max4))
	if err != nil {
		b.Fatal(err)
	}
	defer ownerCompiled.Close()
	owner, err := instantiateCore(ownerCompiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer owner.Close()
	table, err := owner.ExportedTable("table")
	if err != nil {
		b.Fatal(err)
	}
	consumerCompiled, err := compileStagedTable64(table64ImportLifecycleModule(2, &max4))
	if err != nil {
		b.Fatal(err)
	}
	defer consumerCompiled.Close()
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.table": table}})
	if err != nil {
		b.Fatal(err)
	}
	defer consumer.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := consumer.Invoke("size"); err != nil || len(got) != 1 || got[0] != 2 {
			b.Fatalf("imported table64.size = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64CallIndirect(b *testing.B) {
	compiled, err := compileStagedTable64(table64CallIndirectModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("call", 0); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			b.Fatalf("table64 call_indirect = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64InitializedGet(b *testing.B) {
	compiled, err := compileStagedTable64(table64InitializerAndElementModule([]byte{0x42, 0x01}))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("is_null", 0); err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("initialized table64 get = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64FillZero(b *testing.B) {
	compiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("fill", 2, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedTable64GrowZero(b *testing.B) {
	compiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("grow", 0); err != nil || len(got) != 1 || got[0] != 2 {
			b.Fatalf("table64.grow(0) = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedTable64Size(b *testing.B) {
	compiled, err := compileStagedTable64(boundedTable64Module(4))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("size"); err != nil {
			b.Fatal(err)
		}
	}
}

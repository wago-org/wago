//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"testing"
)

// The reference-types/function-references proposals let a funcref/externref
// global or element be initialized with a bottom-type null (ref.null nofunc /
// ref.null noextern), which validation accepts as a subtype of func/extern.
// wat2wasm (wabt) on the test hosts cannot emit these heap types, so the
// fixtures below are assembled by hand. Regression coverage for the three
// const-expr sites that previously whitelisted only func (-16) / extern (-17):
// constexpr.go, frontend's support pass, and element_expr.go.

func TestConstExprBottomRefNullGlobals(t *testing.T) {
	// (module
	//   (global (export "g") funcref   (ref.null nofunc))
	//   (global (export "e") externref (ref.null noextern)))
	mod := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // magic + version
		// global section: 2 immutable reference globals
		0x06, 0x0b, // id=6, size=11
		0x02,                         // count
		0x70, 0x00, 0xd0, 0x73, 0x0b, // funcref, immutable, ref.null nofunc, end
		0x6f, 0x00, 0xd0, 0x72, 0x0b, // externref, immutable, ref.null noextern, end
		// export section: "g" -> global 0, "e" -> global 1
		0x07, 0x09, // id=7, size=9
		0x02,
		0x01, 0x67, 0x03, 0x00, // "g" global 0
		0x01, 0x65, 0x03, 0x01, // "e" global 1
	}
	rt := NewRuntime()
	defer rt.Close()
	m, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("Compile bottom ref-null globals: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), m)
	if err != nil {
		t.Fatalf("Instantiate bottom ref-null globals: %v", err)
	}
	defer in.Close()

	gv, err := in.GlobalValue("g")
	if err != nil {
		t.Fatalf("GlobalValue g: %v", err)
	}
	if gv.Type() != ValFuncRef || !gv.FuncRef().IsNull() {
		t.Fatalf("global g = %v (type %v), want null funcref", gv, gv.Type())
	}
	ev, err := in.GlobalValue("e")
	if err != nil {
		t.Fatalf("GlobalValue e: %v", err)
	}
	if ev.Type() != ValExternRef || !ev.ExternRef().IsNull() {
		t.Fatalf("global e = %v (type %v), want null externref", ev, ev.Type())
	}
}

func TestElementExprBottomRefNull(t *testing.T) {
	// (module
	//   (table 1 funcref)
	//   (elem (table 0) (i32.const 0) funcref (ref.null nofunc)))
	// The active segment's ref.null nofunc expr is parsed by element_expr.go.
	mod := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // magic + version
		// table section: 1 funcref table, min 1
		0x04, 0x04, // id=4, size=4
		0x01, 0x70, 0x00, 0x01,
		// element section: 1 active segment (flag 4), funcref exprs
		0x09, 0x09, // id=9, size=9
		0x01,             // count
		0x04,             // flag 4: active, table 0, expression items
		0x41, 0x00, 0x0b, // offset: i32.const 0; end
		0x01,             // 1 element expr
		0xd0, 0x73, 0x0b, // ref.null nofunc; end
	}
	rt := NewRuntime()
	defer rt.Close()
	m, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("Compile bottom ref-null element: %v", err)
	}
	if _, err := rt.Instantiate(context.Background(), m); err != nil {
		t.Fatalf("Instantiate bottom ref-null element: %v", err)
	}
}

package wago

import "testing"

func TestGlobalRejectsMetadataMutation(t *testing.T) {
	for _, vector := range []bool{false, true} {
		g := NewGlobalI64(42, false)
		if vector {
			g.Close()
			g = NewGlobalV128(V128{42}, false)
		}
		g.Mutable = true
		if err := g.Set(7); err == nil {
			t.Fatal("changed public mutability allowed scalar write")
		}
		if err := g.SetV128(V128{7}); err == nil {
			t.Fatal("changed public mutability allowed vector write")
		}
		g.Mutable = false
		if vector {
			if got := g.GetV128(); got != (V128{42}) {
				t.Fatalf("value changed: %v", got)
			}
		} else if got := g.Get(); got != 42 {
			t.Fatalf("value changed: %v", got)
		}
		g.Close()
	}
	g := NewGlobalV128(V128{42, 1}, true)
	defer g.Close()
	g.Type = ValI64
	if g.Get() != 0 || g.GetV128() != (V128{}) {
		t.Fatal("inconsistent type exposed storage")
	}
	if err := g.Set(7); err == nil {
		t.Fatal("inconsistent type allowed partial write")
	}
	g.Type = ValV128
	if got := g.GetV128(); got != (V128{42, 1}) {
		t.Fatalf("vector changed: %v", got)
	}
}

func TestReferenceGlobalScalarAccessIsOpaque(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	g, err := rt.NewFuncRefGlobal(FuncRef{}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for _, typ := range []ValType{ValFuncRef, ValI64, ValV128} {
		g.Type = typ
		if g.Get() != 0 || g.GetV128() != (V128{}) {
			t.Fatal("numeric accessor exposed reference storage")
		}
		if err := g.Set(0); err == nil {
			t.Fatal("numeric setter accepted reference storage")
		}
		if err := g.SetV128(V128{}); err == nil {
			t.Fatal("vector setter accepted reference storage")
		}
	}
	g.Type = ValFuncRef
}

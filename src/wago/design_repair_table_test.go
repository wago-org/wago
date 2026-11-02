package wago

import (
	"encoding/binary"
	"testing"
)

func TestDesignRepairPublicTableOrdinary(t *testing.T) {
	rt := NewRuntime()
	table, err := rt.NewExternRefTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	ref, err := rt.NewExternRef("ordinary")
	if err != nil {
		t.Fatal(err)
	}
	if err = table.SetValue(0, ValueExternRef(ref)); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint64(table.desc[8:]) != ref.token {
		t.Fatal("ordinary token not stored")
	}
	foreign, err := NewRuntime().NewExternRef("foreign")
	if err != nil {
		t.Fatal(err)
	}
	if err = table.SetValue(0, ValueExternRef(foreign)); err == nil {
		t.Fatal("foreign token accepted")
	}
	if err = table.SetValue(1, ValueExternRef(ref)); err == nil {
		t.Fatal("out of bounds accepted")
	}
	if err = table.SetValue(0, ValueI32(0)); err == nil {
		t.Fatal("wrong type accepted")
	}
	if binary.LittleEndian.Uint64(table.desc[8:]) != ref.token {
		t.Fatal("rejected publication changed slot")
	}
	if err = table.SetValue(0, ValueExternRef(NullExternRef())); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint64(table.desc[8:]) != 0 {
		t.Fatal("null not stored")
	}
}

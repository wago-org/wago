//go:build !(linux || (darwin && arm64)) || riscv64 || tinygo || wago_target_tinygo

package wago

import (
	"encoding/binary"
	"testing"
)

type designRepairConvertedPublicToken struct{}

func (designRepairConvertedPublicToken) externrefPublicValue() any { return uint64(42) }

// Directory authentication alone must not classify compact converted tokens as
// ordinary externrefs. Their public-value projection deliberately hides ownership.
func TestDesignRepairUnsupportedConvertedPublicToken(t *testing.T) {
	rt := NewRuntime()
	table, err := rt.NewExternRefTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	token, err := rt.refStore.issueExternref(designRepairConvertedPublicToken{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rt.refStore.resolveExternref(token); !ok {
		t.Fatal("fixture token is not public")
	}
	if err = table.SetValue(0, Value{typ: ValExternRef, bits: token}); err == nil {
		t.Fatal("STOP converted wrapper accepted as ordinary")
	}
	if binary.LittleEndian.Uint64(table.desc[8:]) != 0 {
		t.Fatal("STOP converted rejection changed storage")
	}
}

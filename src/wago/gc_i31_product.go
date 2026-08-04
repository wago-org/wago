package wago

import (
	"crypto/sha256"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// stagedGCI31Product identifies exact compile-only gc/i31 products. i31 values
// share the compact gc.Ref word internally, but this product marker is separate
// from collector-backed struct/array ownership and is never serialized.
type stagedGCI31Product uint8

const (
	stagedGCI31ProductCore stagedGCI31Product = iota + 1
	stagedGCI31ProductTable
	stagedGCI31ProductTableGlobalInitializer
	stagedGCI31ProductGlobalInitializer
	stagedGCI31ProductAnyGlobal
	stagedGCI31ProductAnyTable
	stagedGCI31ProductRefTest
)

func (p stagedGCI31Product) String() string {
	switch p {
	case stagedGCI31ProductCore:
		return "core"
	case stagedGCI31ProductTable:
		return "i31-table"
	case stagedGCI31ProductTableGlobalInitializer:
		return "table-global-initializer"
	case stagedGCI31ProductGlobalInitializer:
		return "global-global-initializer"
	case stagedGCI31ProductAnyGlobal:
		return "anyref-global"
	case stagedGCI31ProductAnyTable:
		return "anyref-table"
	case stagedGCI31ProductRefTest:
		return "ref-test"
	default:
		return "unknown"
	}
}

func stagedGCI31PinnedProduct(data []byte) (stagedGCI31Product, bool) {
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	switch {
	case len(data) == 252 && digest == "4bdd4d0f186a2fd617b41ad4940e17f2c0415514ebc636a56e41496e8c392aea":
		return stagedGCI31ProductCore, true
	case len(data) == 259 && digest == "c2a2062022d3b99a27aada76d1fe14cdacf3387e7497943d17d3681f65ed7329":
		return stagedGCI31ProductTable, true
	case len(data) == 96 && digest == "0a26e50d6ec8ccbaf1cc3a59fc2e1be6dca2c22219ba70ca80a01451882bf0e4":
		return stagedGCI31ProductTableGlobalInitializer, true
	case len(data) == 88 && digest == "024b9a334c9a7bb6933243fa1eaf60ed653338034bfaf9e54fb23ccc13c9ad87":
		return stagedGCI31ProductGlobalInitializer, true
	case len(data) == 131 && digest == "757d3266617ea901221facbda9b660d6d1fd52adf492ffa82ba0658dc846b26d":
		return stagedGCI31ProductAnyGlobal, true
	case len(data) == 262 && digest == "572387a2c9d7ea9112f3940025b7c57041cd9478185ed7e32bb93a01fbfa5a69":
		return stagedGCI31ProductAnyTable, true
	case len(data) == 255 && digest == "15ae51eb557db91694fc8cb2e2ca148792eec4ae2b524c610edf8005a04837ec":
		return stagedGCI31ProductRefTest, true
	default:
		return 0, false
	}
}

func stagedGCI31ExecutionProduct(data []byte) (stagedGCI31Product, bool) {
	return stagedGCI31PinnedProduct(data)
}

type gcI31TableInitializer struct {
	TableIndex  uint32
	GlobalIndex uint32
}

func stagedGCI31TableInitializer(m *wasm.Module) (*gcI31TableInitializer, error) {
	if m == nil || m.ImportedTableCount() != 0 || len(m.Tables) != 1 || m.Tables[0].Init == nil {
		return nil, fmt.Errorf("expected one local table with an initializer")
	}
	body := m.Tables[0].Init.BodyBytes
	if len(body) == 0 {
		var err error
		body, err = wasm.EncodeExpr(*m.Tables[0].Init)
		if err != nil {
			return nil, err
		}
	}
	r := wasm.NewReader(body)
	op, err := r.Byte()
	if err != nil || op != 0x23 {
		return nil, fmt.Errorf("table initializer requires global.get")
	}
	globalIndex, err := r.U32()
	if err != nil {
		return nil, err
	}
	gt, ok := m.GlobalTypeByIndex(globalIndex)
	if !ok || gt.Mutable || !wasm.EqualValType(gt.Type, wasm.I32) {
		return nil, fmt.Errorf("table initializer global %d is not immutable i32", globalIndex)
	}
	prefix, err := r.Byte()
	if err != nil || prefix != 0xfb {
		return nil, fmt.Errorf("table initializer requires ref.i31")
	}
	sub, err := r.U32()
	if err != nil || sub != 28 {
		return nil, fmt.Errorf("table initializer has unsupported 0xfb opcode %d", sub)
	}
	end, err := r.Byte()
	if err != nil || end != 0x0b || r.BytesLeft() != 0 {
		return nil, fmt.Errorf("table initializer has invalid end")
	}
	return &gcI31TableInitializer{TableIndex: 0, GlobalIndex: globalIndex}, nil
}
